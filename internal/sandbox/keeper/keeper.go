package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"crdx.org/oh/internal/sandbox/testnamespace"

	"crdx.org/oh/internal/util"

	"golang.org/x/sys/unix"
)

const (
	envKeeper   = "IO_KEEPER"
	executable  = "/proc/self/exe"
	keeperName  = "oh (keeper)"
	commandName = "oh (command)"

	controlDescriptor = 3
	messageBytes      = 1 << 16
	readyTimeout      = 10 * time.Second
	noticeBytes       = 8 << 10
)

var ErrClosed = errors.New("the keeper is closed")

type Status struct {
	ExitCode   int
	Signal     syscall.Signal
	CPUTime    time.Duration
	PeakMemory uint64
}

func Attributes() *syscall.SysProcAttr {
	flags := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET)

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

func CommandAttributes() *syscall.SysProcAttr {
	if testnamespace.IsUnmapped() {
		return &syscall.SysProcAttr{
			Setpgid:   true,
			Pdeathsig: syscall.SIGKILL,
		}
	}

	return &syscall.SysProcAttr{
		Setpgid:    true,
		Pdeathsig:  syscall.SIGKILL,
		Cloneflags: uintptr(syscall.CLONE_NEWPID | syscall.CLONE_NEWNS),
	}
}

func Init() {
	if os.Getenv(envKeeper) == "" {
		return
	}

	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "keeper:", err)
		os.Exit(125)
	}

	os.Exit(0)
}

type Keeper struct {
	process *exec.Cmd
	control *net.UnixConn
	notice  notes

	writeMutex   sync.Mutex
	answersMutex sync.Mutex
	answers      map[uint64]chan arrival
	nextID       atomic.Uint64
	isClosed     atomic.Bool
	readers      sync.WaitGroup

	portDirectionsMutex sync.Mutex
	sandboxToHost       map[uint16]*bridge
	hostToSandbox       map[uint16]*bridge
}

type arrival struct {
	answer   reply
	handover *os.File
}

func Open(ctx context.Context) (*Keeper, error) {
	descriptors, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("could not open the control socket: %w", err)
	}

	near := os.NewFile(uintptr(descriptors[0]), "keeper-control")
	far := os.NewFile(uintptr(descriptors[1]), "keeper-control")
	defer func() { _ = far.Close() }()

	control, err := connection(near)
	if err != nil {
		return nil, err
	}

	self := &Keeper{control: control, answers: make(map[uint64]chan arrival)}

	process := exec.CommandContext(context.WithoutCancel(ctx), executable)
	process.Args = []string{keeperName}
	process.Env = append([]string{envKeeper + "=1"}, testnamespace.Environment()...)
	process.ExtraFiles = []*os.File{far}
	process.SysProcAttr = Attributes()
	process.Stderr = &self.notice
	self.process = process

	if err := process.Start(); err != nil {
		_ = control.Close()
		return nil, fmt.Errorf("could not start the keeper: %w", err)
	}

	if err := self.awaitReady(); err != nil {
		_ = self.Close()
		return nil, err
	}

	self.readers.Add(1)

	go self.receive()

	return self, nil
}

func (self *Keeper) Spawn(
	ctx context.Context,
	directory string,
	environment []string,
	output io.Writer,
) (*Process, error) {
	if self.isClosed.Load() {
		return nil, ErrClosed
	}

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("could not open the command's output: %w", err)
	}

	id := self.nextID.Add(1)
	answers := make(chan arrival, 2)

	self.answersMutex.Lock()
	self.answers[id] = answers
	self.answersMutex.Unlock()

	err = self.ask(request{
		Kind:        requestSpawn,
		Token:       id,
		Directory:   directory,
		Environment: environment,
	}, writeEnd)
	_ = writeEnd.Close()

	if err != nil {
		self.forget(id)
		_ = readEnd.Close()

		return nil, err
	}

	drainedOutput := make(chan struct{})

	go func() {
		defer close(drainedOutput)
		defer func() { _ = readEnd.Close() }()
		_, _ = io.Copy(output, readEnd)
	}()

	var packet arrival
	var isOpen bool

	select {
	case packet, isOpen = <-answers:
	case <-ctx.Done():
		self.forget(id)
		<-drainedOutput

		return nil, ctx.Err()
	}

	if !isOpen || packet.answer.Kind == replyRefused {
		self.forget(id)
		<-drainedOutput

		if !isOpen {
			return nil, ErrClosed
		}

		return nil, errors.New(packet.answer.Failure)
	}

	return &Process{keeper: self, token: id, answers: answers, drainedOutput: drainedOutput}, nil
}

func (self *Keeper) Close() error {
	if self.isClosed.Swap(true) {
		return nil
	}

	_ = self.control.Close()
	self.closeHostToSandbox()
	self.closeSandboxToHost()
	self.readers.Wait()

	if self.process.Process != nil {
		_ = syscall.Kill(-self.process.Process.Pid, syscall.SIGKILL)
		_ = self.process.Wait()
	}

	return nil
}

func (self *Keeper) awaitReady() error {
	if err := self.control.SetReadDeadline(time.Now().Add(readyTimeout)); err != nil {
		return err
	}

	message := make([]byte, messageBytes)
	length, _, flags, _, err := self.control.ReadMsgUnix(message, nil)
	if err != nil {
		return fmt.Errorf("the keeper did not start: %s", self.refusal(err))
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return errors.New("the keeper's ready message did not arrive whole")
	}

	var answer reply
	if err := json.Unmarshal(message[:length], &answer); err != nil || answer.Kind != replyReady {
		return fmt.Errorf("the keeper did not start: %s", self.refusal(err))
	}

	return self.control.SetReadDeadline(time.Time{})
}

func (self *Keeper) refusal(err error) string {
	if wording := strings.TrimSpace(self.notice.String()); wording != "" {
		return strings.TrimPrefix(wording, "keeper: ")
	}

	if err == nil {
		return "it said nothing"
	}

	return err.Error()
}

func (self *Keeper) receive() {
	defer self.readers.Done()

	message := make([]byte, messageBytes)
	control := make([]byte, unix.CmsgSpace(4))

	for {
		length, controlLength, _, _, err := self.control.ReadMsgUnix(message, control)
		if err != nil {
			self.abandon()
			return
		}

		files := parseFiles(control[:controlLength])

		var answer reply
		if err := json.Unmarshal(message[:length], &answer); err != nil {
			closeFiles(files)
			continue
		}

		self.answersMutex.Lock()
		answers, isAwaited := self.answers[answer.Token]
		self.answersMutex.Unlock()

		if !isAwaited {
			closeFiles(files)
			continue
		}

		packet := arrival{answer: answer}
		if len(files) > 0 {
			packet.handover = files[0]
			closeFiles(files[1:])
		}

		answers <- packet
	}
}

func (self *Keeper) abandon() {
	self.answersMutex.Lock()
	defer self.answersMutex.Unlock()

	for token, answers := range self.answers {
		close(answers)
		delete(self.answers, token)
	}
}

func (self *Keeper) ask(instruction request, handover *os.File) error {
	payload, err := json.Marshal(instruction)
	if err != nil {
		return err
	}

	if len(payload) > messageBytes {
		return fmt.Errorf(
			"the command is too long to reach the keeper: %s of instruction against a limit of %s",
			util.FormatBytes(int64(len(payload)), 1), util.FormatBytes(messageBytes, 1),
		)
	}

	var rights []byte
	if handover != nil {
		rights = unix.UnixRights(int(handover.Fd()))
	}

	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()

	if _, _, err := self.control.WriteMsgUnix(payload, rights, nil); err != nil {
		return fmt.Errorf("could not reach the keeper: %w", err)
	}

	return nil
}

func (self *Keeper) forget(token uint64) {
	self.answersMutex.Lock()
	defer self.answersMutex.Unlock()
	delete(self.answers, token)
}

type Process struct {
	keeper        *Keeper
	token         uint64
	answers       chan arrival
	drainedOutput chan struct{}
}

func (self *Process) Wait() (Status, error) {
	packet, isOpen := <-self.answers
	<-self.drainedOutput
	self.keeper.forget(self.token)

	if !isOpen {
		return Status{}, ErrClosed
	}

	answer := packet.answer
	status := Status{
		ExitCode:   answer.ExitCode,
		Signal:     syscall.Signal(answer.Signal),
		CPUTime:    answer.CPUTime,
		PeakMemory: answer.PeakMemory,
	}

	if answer.Failure != "" {
		return status, errors.New(answer.Failure)
	}

	return status, nil
}

func (self *Process) Signal(signal syscall.Signal) error {
	return self.keeper.ask(request{Kind: requestSignal, Token: self.token, Signal: int(signal)}, nil)
}

type notes struct {
	writeMutex sync.Mutex
	text       strings.Builder
}

func (self *notes) Write(data []byte) (int, error) {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()

	if room := noticeBytes - self.text.Len(); room > 0 {
		_, _ = self.text.Write(data[:min(room, len(data))])
	}

	return len(data), nil
}

func (self *notes) String() string {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()

	return self.text.String()
}

func connection(file *os.File) (*net.UnixConn, error) {
	defer func() { _ = file.Close() }()

	socket, err := net.FileConn(file)
	if err != nil {
		return nil, fmt.Errorf("could not open the control socket: %w", err)
	}

	control, isUnix := socket.(*net.UnixConn)
	if !isUnix {
		_ = socket.Close()
		return nil, errors.New("the control socket is not a Unix socket")
	}

	return control, nil
}

func serve() error {
	runtime.LockOSThread()

	control, err := connection(os.NewFile(controlDescriptor, "keeper-control"))
	if err != nil {
		return err
	}
	defer func() { _ = control.Close() }()

	service := &service{control: control, commands: make(map[uint64]*exec.Cmd)}

	if err := service.send(reply{Kind: replyReady}); err != nil {
		return err
	}

	service.accept()
	service.abandonEverything()
	service.runners.Wait()

	return nil
}

func (self *service) abandonEverything() {
	self.commandsMutex.Lock()
	runningCommands := make([]*exec.Cmd, 0, len(self.commands))
	for _, command := range self.commands {
		runningCommands = append(runningCommands, command)
	}
	self.commandsMutex.Unlock()

	for _, command := range runningCommands {
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
	}
}

type service struct {
	control       *net.UnixConn
	writeMutex    sync.Mutex
	commandsMutex sync.Mutex
	commands      map[uint64]*exec.Cmd
	runners       sync.WaitGroup
	loopbackOnce  sync.Once
	loopbackErr   error
}

func (self *service) accept() {
	message := make([]byte, messageBytes)
	control := make([]byte, unix.CmsgSpace(4))

	for {
		messageLength, controlLength, flags, _, err := self.control.ReadMsgUnix(message, control)
		if err != nil {
			return
		}

		output := received(control[:controlLength])

		self.take(message[:messageLength], flags, output)

		if output != nil {
			_ = output.Close()
		}
	}
}

func (self *service) take(message []byte, flags int, output *os.File) {
	var instruction request
	if err := json.Unmarshal(message, &instruction); err != nil {
		return
	}

	if flags&unix.MSG_TRUNC != 0 {
		_ = self.send(reply{
			Kind:    replyRefused,
			Token:   instruction.Token,
			Failure: "the instruction was too long to arrive whole",
		})

		return
	}

	switch instruction.Kind {
	case requestSpawn:
		self.spawn(instruction, output)
	case requestSignal:
		self.kill(instruction)
	case requestHostToSandboxDial:
		self.dialHostToSandbox(instruction)
	case requestSandboxToHostListen:
		self.listenSandboxToHost(instruction)
	}
}

func received(control []byte) *os.File {
	if len(control) == 0 {
		return nil
	}

	messages, err := unix.ParseSocketControlMessage(control)
	if err != nil {
		return nil
	}

	var output *os.File

	for _, message := range messages {
		descriptors, err := unix.ParseUnixRights(&message)
		if err != nil {
			continue
		}

		for _, descriptor := range descriptors {
			unix.CloseOnExec(descriptor)

			if output == nil {
				output = os.NewFile(uintptr(descriptor), "command-output")
				continue
			}

			_ = unix.Close(descriptor)
		}
	}

	return output
}

func (self *service) spawn(instruction request, output *os.File) {
	if output == nil {
		_ = self.send(reply{Kind: replyRefused, Token: instruction.Token, Failure: "no output was passed"})
		return
	}

	command := exec.CommandContext(context.Background(), executable)
	command.Args = []string{commandName}
	command.Dir = instruction.Directory
	command.Stdout = output
	command.Stderr = output
	command.Env = instruction.Environment
	command.SysProcAttr = CommandAttributes()

	if err := command.Start(); err != nil {
		_ = self.send(reply{Kind: replyRefused, Token: instruction.Token, Failure: err.Error()})
		return
	}

	self.commandsMutex.Lock()
	self.commands[instruction.Token] = command
	self.commandsMutex.Unlock()

	if err := self.send(reply{Kind: replySpawned, Token: instruction.Token}); err != nil {
		return
	}

	self.runners.Add(1)

	go self.reap(instruction.Token, command)
}

func (self *service) reap(token uint64, command *exec.Cmd) {
	defer self.runners.Done()

	err := command.Wait()

	self.commandsMutex.Lock()
	delete(self.commands, token)
	self.commandsMutex.Unlock()

	answer := reply{Kind: replyFinished, Token: token}

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		answer.Failure = err.Error()
	}

	if state := command.ProcessState; state != nil {
		answer.ExitCode = state.ExitCode()
		answer.CPUTime = state.UserTime() + state.SystemTime()

		if status, isStatus := state.Sys().(syscall.WaitStatus); isStatus && status.Signaled() {
			answer.Signal = int(status.Signal())
		}
		if usage, isUsage := state.SysUsage().(*syscall.Rusage); isUsage && usage.Maxrss > 0 {
			answer.PeakMemory = uint64(usage.Maxrss) * 1024
		}
	}

	_ = self.send(answer)
}

func (self *service) kill(instruction request) {
	self.commandsMutex.Lock()
	defer self.commandsMutex.Unlock()

	command, isKnown := self.commands[instruction.Token]
	if !isKnown || command.Process == nil || command.ProcessState != nil {
		return
	}

	_ = syscall.Kill(-command.Process.Pid, syscall.Signal(instruction.Signal))
}

func (self *service) send(answer reply) error {
	return self.sendFiles(answer, nil)
}

func (self *service) sendFiles(answer reply, files []*os.File) error {
	payload, err := json.Marshal(answer)
	if err != nil {
		return err
	}

	var rights []byte
	if len(files) > 0 {
		descriptors := make([]int, len(files))
		for i, file := range files {
			descriptors[i] = int(file.Fd())
		}
		rights = unix.UnixRights(descriptors...)
	}

	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()

	_, _, err = self.control.WriteMsgUnix(payload, rights, nil)
	return err
}
