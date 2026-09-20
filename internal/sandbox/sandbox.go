package sandbox

import (
	"maps"
	"slices"
	"time"

	"crdx.org/oh/internal/util/pathutil"
)

var systemPathGrants = []grant{
	{path: "/usr", rights: rightsExec, isOptional: true},
	{path: "/bin", rights: rightsExec, isOptional: true},
	{path: "/sbin", rights: rightsExec, isOptional: true},
	{path: "/lib", rights: rightsExec, isOptional: true},
	{path: "/lib64", rights: rightsExec, isOptional: true},
	{path: "/etc/ld.so.cache", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.conf.d", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.preload", rights: rightsRead, isOptional: true},
	{path: "/etc/fonts", rights: rightsRead, isOptional: true},
	{path: "/etc/ssl/openssl.cnf", rights: rightsRead, isOptional: true},
	{path: "/etc/ssl/certs", rights: rightsRead, isOptional: true},
	{path: "/etc/ca-certificates", rights: rightsRead, isOptional: true},
	{path: "/etc/pki", rights: rightsRead, isOptional: true},
	{path: "/etc/nsswitch.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/gai.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/services", rights: rightsRead, isOptional: true},
	{path: "/etc/protocols", rights: rightsRead, isOptional: true},
	{path: "/etc/passwd", rights: rightsRead, isOptional: true},
	{path: "/etc/group", rights: rightsRead, isOptional: true},
	{path: "/etc/localtime", rights: rightsRead, isOptional: true},
	{path: "/etc/timezone", rights: rightsRead, isOptional: true},
	{path: "/etc/locale.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/gitconfig", rights: rightsRead, isOptional: true},
	{path: "/etc/shells", rights: rightsRead, isOptional: true},
	{path: "/etc/machine-id", rights: rightsRead, isOptional: true},
	{path: "/etc/profile", rights: rightsRead, isOptional: true},
	{path: "/etc/profile.d", rights: rightsRead, isOptional: true},
	{path: "/etc/bash.bashrc", rights: rightsRead, isOptional: true},
	{path: "/etc/bash.bash_logout", rights: rightsRead, isOptional: true},
	{path: "/etc/inputrc", rights: rightsRead, isOptional: true},
	{path: "/etc/login.defs", rights: rightsRead, isOptional: true},
	{path: "/etc/xdg", rights: rightsRead, isOptional: true},
	{path: "/etc/mime.types", rights: rightsRead, isOptional: true},
	{path: "/etc/os-release", rights: rightsRead, isOptional: true},
	{path: "/etc/terminfo", rights: rightsRead, isOptional: true},
	{path: "/dev/null", rights: rightsWrite, isOptional: true},
	{path: "/dev/zero", rights: rightsRead, isOptional: true},
	{path: "/dev/full", rights: rightsWrite, isOptional: true},
	{path: "/dev/random", rights: rightsRead, isOptional: true},
	{path: "/dev/urandom", rights: rightsRead, isOptional: true},
}

func BaselineReadablePaths() []string {
	paths := make([]string, 0, len(systemPathGrants))
	for _, systemGrant := range systemPathGrants {
		if systemGrant.rights&rightsRead == rightsRead {
			paths = append(paths, systemGrant.path)
		}
	}
	return paths
}

type Policy struct {
	Yolo    bool `json:"yolo,omitempty"`
	Network bool `json:"network,omitempty"`

	Deny          []string          `json:"deny,omitempty"`
	DenyPaths     []string          `json:"deny_paths,omitempty"`
	Read          []string          `json:"read"`
	Write         []string          `json:"write"`
	Sockets       []string          `json:"sockets"`
	Exec          []string          `json:"exec"`
	OptionalPaths []string          `json:"optional_paths,omitempty"`
	TmpDir        string            `json:"tmpdir"`
	Env           []string          `json:"env"`
	SetEnv        map[string]string `json:"set_env"`
	Timeout       time.Duration     `json:"timeout"`

	MaxCPUTime   time.Duration `json:"cpu_time"`
	MaxFileSize  int64         `json:"file_size"`
	MaxOpenFiles int64         `json:"open_files"`
	MaxProcesses int64         `json:"processes"`
}

func (self Policy) WithRead(paths ...string) Policy {
	self.Read = append(slices.Clone(self.Read), paths...)
	return self
}

func (self Policy) WithoutRead(paths ...string) Policy {
	self.Read = slices.DeleteFunc(slices.Clone(self.Read), func(path string) bool {
		return slices.Contains(paths, path)
	})
	return self
}

func (self Policy) WithWrite(paths ...string) Policy {
	self.Write = append(slices.Clone(self.Write), paths...)
	return self
}

func (self Policy) WithSetEnv(name string, value string) Policy {
	self.SetEnv = maps.Clone(self.SetEnv)
	if self.SetEnv == nil {
		self.SetEnv = make(map[string]string)
	}
	self.SetEnv[name] = value
	return self
}

func (self Policy) Writable() bool {
	for _, path := range self.Write {
		if path != TmpDir {
			return true
		}
	}

	return false
}

type grant struct {
	path       string
	rights     uint64
	isOptional bool
}

func (self Policy) grants() []grant {
	grants := make([]grant, 0, len(systemPathGrants)+len(self.Read)+len(self.Write)+len(self.Exec)+2)
	grants = append(grants, systemPathGrants...)

	grants = append(
		grants,
		grant{path: "/dev/ptmx", rights: rightsWrite},
		grant{path: "/dev/pts", rights: rightsWrite},
		grant{path: processFilesystemPath, rights: rightsRead},
	)

	for _, file := range resolverFiles {
		grants = append(grants, grant{path: file.path, rights: rightsRead})
	}

	for _, path := range self.Read {
		grants = append(grants, grant{
			path: path, rights: rightsRead, isOptional: slices.Contains(self.OptionalPaths, path),
		})
	}

	for _, path := range self.Exec {
		grants = append(grants, grant{
			path: path, rights: rightsExec, isOptional: slices.Contains(self.OptionalPaths, path),
		})
	}

	for _, path := range self.Write {
		rights := uint64(rightsWrite)
		if slices.Contains(self.Sockets, path) {
			rights |= accessResolveUnix
		}

		grants = append(grants, grant{
			path: path, rights: rights, isOptional: slices.Contains(self.OptionalPaths, path),
		})
	}

	return grants
}

func (self Policy) missingPaths() []string {
	var absent []string

	if self.TmpDir != "" && !pathutil.Exists(self.TmpDir) {
		absent = append(absent, self.TmpDir)
	}

	for _, grant := range self.grants() {
		if !grant.isOptional && !pathutil.Exists(grant.path) {
			absent = append(absent, grant.path)
		}
	}
	return absent
}
