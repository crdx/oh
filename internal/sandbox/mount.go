package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"crdx.org/hereduck"
	"crdx.org/oh/internal/sandbox/testnamespace"
	"crdx.org/oh/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

type mountRefinement struct {
	path       string
	isReadOnly bool
}

func (self Policy) mountRefinements() []mountRefinement {
	isWritable := make(map[string]bool, len(self.Read)+len(self.Write))
	for _, path := range self.Read {
		isWritable[filepath.Clean(path)] = false
	}
	for _, path := range self.Write {
		isWritable[filepath.Clean(path)] = true
	}

	paths := make([]string, 0, len(isWritable))
	for path := range isWritable {
		paths = append(paths, path)
	}
	slices.SortFunc(paths, func(left string, right string) int {
		if difference := len(left) - len(right); difference != 0 {
			return difference
		}
		return strings.Compare(left, right)
	})

	var refinements []mountRefinement
	for _, path := range paths {
		isCurrentlyReadOnly := false
		for _, refinement := range refinements {
			if _, isBelow := pathutil.RelativeTo(refinement.path, path); isBelow {
				isCurrentlyReadOnly = refinement.isReadOnly
			}
		}

		isReadOnlyWanted := !isWritable[path] && self.writeCovers(path)
		if isReadOnlyWanted != isCurrentlyReadOnly {
			refinements = append(refinements, mountRefinement{path: path, isReadOnly: isReadOnlyWanted})
		}
	}

	return refinements
}

func (self Policy) writeCovers(path string) bool {
	for _, write := range self.Write {
		if _, isBelow := pathutil.RelativeTo(write, path); isBelow {
			return true
		}
	}
	return false
}

const privateNamespaces = uintptr(
	syscall.CLONE_NEWUSER |
		syscall.CLONE_NEWPID |
		syscall.CLONE_NEWNS |
		syscall.CLONE_NEWIPC |
		syscall.CLONE_NEWUTS,
)

const processFilesystemPath = "/proc"

const TmpDir = "/tmp"

type virtualFile struct {
	path     string
	contents string
}

var resolvconfContents = hereduck.D(`
	# managed by oh
	nameserver 127.0.0.1
`)

var hostsContents = hereduck.D(`
	127.0.0.1 localhost
	::1 localhost ip6-localhost ip6-loopback
`)

var nssContents = hereduck.D(`
	passwd: files
	group: files
	shadow: files
	hosts: files dns
	networks: files
	protocols: files
	services: files
	ethers: files
	rpc: files
`)

var resolverFiles = []virtualFile{
	{path: "/etc/resolv.conf", contents: resolvconfContents},
	{path: "/etc/hosts", contents: hostsContents},
	{path: "/etc/nsswitch.conf", contents: nssContents},
}

var probedNamespaces sync.Map

func checkNamespaces(ctx context.Context) error {
	isUnmapped := testnamespace.IsUnmapped()

	if _, wasProbed := probedNamespaces.Load(isUnmapped); wasProbed {
		return nil
	}

	if err := probeNamespaces(ctx); err != nil {
		return err
	}

	probedNamespaces.Store(isUnmapped, struct{}{})

	return nil
}

func probeNamespaces(ctx context.Context) error {
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	probe := namespaceProbeCommand(probeContext)
	output, err := probe.CombinedOutput()
	message := strings.TrimSpace(strings.TrimPrefix(string(output), notice))
	if err != nil {
		if message != "" {
			return fmt.Errorf("this machine will not give the sandbox its namespaces: %s", message)
		}
		return fmt.Errorf("this machine will not give the sandbox its namespaces: %w", err)
	}
	if !saysProbeSucceeded(output) {
		return errors.New("the executable did not initialise the sandbox namespace probe")
	}

	return nil
}

func saysProbeSucceeded(output []byte) bool {
	for line := range strings.SplitSeq(string(output), "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, notice)) == probeSucceeded {
			return true
		}
	}

	return false
}

func namespaceProbeCommand(ctx context.Context) *exec.Cmd {
	probe := exec.CommandContext(ctx, executable, "-test.run=^$")
	probe.Args = []string{probeName, "-test.run=^$"}
	probe.Env = append([]string{envProbe + "=1"}, testnamespace.Environment()...)
	probe.SysProcAttr = namespaceAttributes()
	return probe
}

func applyMounts(policy Policy) error {
	if testnamespace.IsUnmapped() {
		return nil
	}

	if err := mountProcessFilesystem(); err != nil {
		return err
	}

	if err := mountPseudoterminals(); err != nil {
		return err
	}

	for _, refinement := range policy.mountRefinements() {
		isOptional := slices.Contains(policy.OptionalPaths, refinement.path)
		if isOptional && !pathutil.Exists(refinement.path) {
			continue
		}
		if err := mountWithAccess(refinement); err != nil {
			if isOptional && !pathutil.Exists(refinement.path) {
				continue
			}
			return err
		}
	}

	if !policy.Network {
		for _, file := range resolverFiles {
			if err := mountReadOnlyTextFile(file.path, file.contents); err != nil {
				return err
			}
		}
	}

	if policy.TmpDir != "" {
		if err := attach(policy.TmpDir, TmpDir, nil); err != nil {
			return err
		}
	}

	denialMounts, err := prepareDenialMounts(policy.DenyPaths)
	if err != nil {
		return err
	}
	defer closeDenialMounts(denialMounts)
	return installDenialMounts(denialMounts)
}

type denialMount struct {
	path        string
	backingPath string
	fd          int
}

func prepareDenialMounts(paths []string) ([]denialMount, error) {
	mounts := make([]denialMount, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			closeDenialMounts(mounts)
			return nil, fmt.Errorf("could not inspect denied path %s: %w", path, err)
		}

		backingPath, err := makeDenialBacking(info.IsDir())
		if err != nil {
			closeDenialMounts(mounts)
			return nil, fmt.Errorf("could not prepare denied path %s: %w", path, err)
		}
		fd, openErr := unix.OpenTree(unix.AT_FDCWD, backingPath, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
		if openErr != nil {
			_ = os.Remove(backingPath)
			closeDenialMounts(mounts)
			return nil, fmt.Errorf("could not prepare denied path %s: %w", path, openErr)
		}
		mounts = append(mounts, denialMount{path: path, backingPath: backingPath, fd: fd})
	}
	return mounts, nil
}

func makeDenialBacking(isDirectory bool) (string, error) {
	if isDirectory {
		path, err := os.MkdirTemp("", "sandbox-deny-")
		if err != nil {
			return "", err
		}
		if err := os.Chmod(path, 0); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}

	file, err := os.CreateTemp("", "sandbox-deny-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func installDenialMounts(mounts []denialMount) error {
	for _, mount := range mounts {
		if !pathutil.Exists(mount.path) {
			continue
		}
		if err := unix.MoveMount(mount.fd, "", unix.AT_FDCWD, mount.path, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
			return fmt.Errorf("could not deny access to %s: %w", mount.path, err)
		}
	}
	return nil
}

func closeDenialMounts(mounts []denialMount) {
	for _, mount := range mounts {
		_ = unix.Close(mount.fd)
		_ = os.Remove(mount.backingPath)
	}
}

func mountReadOnlyTextFile(path string, contents string) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %w", path, err)
	}

	backingFile, err := writeTemporaryFile(contents)
	if err != nil {
		return fmt.Errorf("could not prepare the contents of %s: %w", path, err)
	}

	mountErr := attach(backingFile, target, &unix.MountAttr{Attr_set: unix.MOUNT_ATTR_RDONLY})
	removeErr := os.Remove(backingFile)

	if mountErr != nil {
		return fmt.Errorf("could not put the prepared contents at %s: %w", path, mountErr)
	}
	if removeErr != nil {
		return fmt.Errorf("could not discard the contents prepared for %s: %w", path, removeErr)
	}

	return nil
}

func writeTemporaryFile(contents string) (string, error) {
	file, err := os.CreateTemp("", "sandbox-virtual-file-")
	if err != nil {
		return "", err
	}

	if _, err := file.WriteString(contents); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}

	return file.Name(), nil
}

func mountWithAccess(refinement mountRefinement) error {
	attributes := &unix.MountAttr{}
	if refinement.isReadOnly {
		attributes.Attr_set = unix.MOUNT_ATTR_RDONLY
	} else {
		attributes.Attr_clr = unix.MOUNT_ATTR_RDONLY
	}
	return attach(refinement.path, refinement.path, attributes)
}

func mountProcessFilesystem() error {
	flags := uintptr(unix.MS_RDONLY | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC)
	if err := unix.Mount("proc", processFilesystemPath, "proc", flags, ""); err != nil {
		return fmt.Errorf("could not mount the private process filesystem: %w", err)
	}

	return nil
}

func mountPseudoterminals() error {
	flags := uintptr(unix.MS_NOSUID | unix.MS_NOEXEC)
	options := "newinstance,ptmxmode=0666,mode=0620,gid=0"
	if err := unix.Mount("devpts", "/dev/pts", "devpts", flags, options); err != nil {
		return fmt.Errorf("could not mount private pseudoterminals: %w", err)
	}
	if err := unix.Mount("/dev/pts/ptmx", "/dev/ptmx", "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("could not attach the pseudoterminal multiplexer: %w", err)
	}
	return nil
}

func attach(source string, target string, attributes *unix.MountAttr) error {
	const clone = unix.OPEN_TREE_CLONE | unix.OPEN_TREE_CLOEXEC

	fd, err := unix.OpenTree(unix.AT_FDCWD, source, clone)
	if err != nil {
		return fmt.Errorf("could not copy the mount at %s: %w", source, err)
	}

	defer func() { _ = unix.Close(fd) }()

	if attributes != nil {
		if err := unix.MountSetattr(fd, "", unix.AT_EMPTY_PATH, attributes); err != nil {
			return fmt.Errorf("could not set the attributes of %s: %w", source, err)
		}
	}

	err = unix.MoveMount(fd, "", unix.AT_FDCWD, target, unix.MOVE_MOUNT_F_EMPTY_PATH)
	if err != nil {
		return fmt.Errorf("could not put %s at %s: %w", source, target, err)
	}

	return nil
}

const lastCapability = 63

func dropCapabilities() error {
	if testnamespace.IsUnmapped() {
		return nil
	}

	for capability := range lastCapability + 1 {
		err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0)
		if err != nil && !errors.Is(err, unix.EINVAL) {
			return fmt.Errorf("could not drop capability %d: %w", capability, err)
		}
	}

	return nil
}

func namespaceAttributes() *syscall.SysProcAttr {
	return namespaceAttributesFor(privateNamespaces | syscall.CLONE_NEWNET)
}

func hostNetworkAttributes() *syscall.SysProcAttr {
	return namespaceAttributesFor(privateNamespaces)
}

func namespaceAttributesFor(flags uintptr) *syscall.SysProcAttr {
	if testnamespace.IsUnmapped() {
		return &syscall.SysProcAttr{
			Setpgid:    true,
			Pdeathsig:  syscall.SIGKILL,
			Cloneflags: flags,
		}
	}

	return &syscall.SysProcAttr{
		Setpgid:     true,
		Pdeathsig:   syscall.SIGKILL,
		Cloneflags:  flags,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}
}
