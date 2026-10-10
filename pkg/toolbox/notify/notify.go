package notify

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tool"
)

const (
	applicationName = "oh"
	focusedResult   = "notification not sent because the terminal is focused"
	iconChoices     = "success, info, warning, error, question, progress"
)

var desktopIconNames = map[string]string{
	"success":  "emblem-default",
	"info":     "dialog-information",
	"warning":  "dialog-warning",
	"error":    "dialog-error",
	"question": "dialog-question",
	"progress": "process-working",
}

type EscapeWriter func(escape string) bool

type Expiry int

const (
	ExpiresAsUsual Expiry = iota
	NeverExpires
)

type Notification struct {
	identifier string
	isEscape   bool
	isSent     bool
}

func (self Notification) IsSent() bool {
	return self.isSent
}

func (self Notification) Withdraw(ctx context.Context, writeEscape EscapeWriter) error {
	if self.identifier == "" {
		return nil
	}

	if self.isEscape {
		if writeEscape == nil || !writeEscape("\x1b]99;i="+self.identifier+":p=close;\x1b\\") {
			return errors.New("withdrawing the notification failed: its terminal is gone")
		}

		return nil
	}

	//nolint:gosec // the executable and options are fixed, and the identifier is a number
	command := exec.CommandContext(
		ctx,
		"gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.CloseNotification",
		self.identifier,
	)
	if err := command.Run(); err != nil {
		return fmt.Errorf("withdrawing the notification failed: %w", err)
	}

	return nil
}

func IsAvailable() bool {
	if isKitty() {
		_, err := exec.LookPath("kitten")
		return err == nil
	}

	_, err := exec.LookPath("notify-send")
	return err == nil
}

func Command(
	ctx context.Context,
	title string,
	message string,
	icon string,
	expiry Expiry,
	identifier string,
) (*exec.Cmd, bool) {
	options := []string{"--icon=" + icon, "--app-name=" + applicationName}

	if isKitty() {
		if expiry == NeverExpires {
			options = append(options, "--expire-after=never")
		}
		if identifier != "" {
			options = append(options, "--identifier="+identifier)
		}
		arguments := append([]string{"notify", "--only-print-escape-code"}, options...)
		//nolint:gosec // the executable and options are fixed, and the arguments are inert
		return exec.CommandContext(ctx, "kitten", append(arguments, "--", title, message)...), true
	}

	if expiry == NeverExpires {
		options = append(options, "--expire-time=0")
	}
	if identifier != "" {
		options = append(options, "--print-id")
	}
	//nolint:gosec // the executable and options are fixed, and the arguments are inert
	return exec.CommandContext(ctx, "notify-send", append(options, "--", title, message)...), false
}

func isKitty() bool {
	return os.Getenv("KITTY_WINDOW_ID") != ""
}

type Args struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Icon    string `json:"icon"`
}

func New(writeEscape EscapeWriter, isTerminalFocused func() bool) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "notify",
			Description: "send a desktop notification when the terminal is not focused",
			Schema: tool.Schema{
				tool.String("title", "notification title, as plain text"),
				tool.String("message", "notification text, as plain text: write < and & as themselves, never as HTML entities"),
				tool.String("icon", "notification icon: (one of "+iconChoices+")"),
			},
		},
		Describe,
	).
		Validate(validate).
		Plain(func(ctx context.Context, args Args) (string, error) {
			return run(ctx, writeEscape, isTerminalFocused, args)
		})
}

func Describe(args Args) tool.CallRendering {
	return tool.CallRendering{Subject: args.Title, Qualifier: "— " + args.Message, ReportsStatus: true}
}

func validate(args Args) error {
	if strings.TrimSpace(args.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(args.Message) == "" {
		return errors.New("message is required")
	}
	if _, isKnown := desktopIconNames[args.Icon]; !isKnown {
		return fmt.Errorf("icon must be one of: %s", iconChoices)
	}

	return nil
}

func Send(ctx context.Context, writeEscape EscapeWriter, args Args, expiry Expiry) (Notification, error) {
	if err := validate(args); err != nil {
		return Notification{}, err
	}

	var identifier string
	if expiry == NeverExpires {
		identifier = rand.Text()
	}

	command, printsEscapeCode := Command(ctx, args.Title, args.Message, desktopIconNames[args.Icon], expiry, identifier)

	var output strings.Builder
	command.Stdout = &output
	if printsEscapeCode && writeEscape == nil {
		return Notification{}, errors.New("notification failed: nothing to write it to")
	}

	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Notification{}, stop.Error(ctx, "the notification")
		}

		return Notification{}, fmt.Errorf("notification failed: %w", err)
	}

	if printsEscapeCode {
		if !writeEscape(output.String()) {
			return Notification{}, errors.New("notification failed: its terminal is gone")
		}

		return Notification{identifier: identifier, isEscape: true, isSent: true}, nil
	}

	if identifier == "" {
		return Notification{isSent: true}, nil
	}

	return Notification{identifier: printedIdentifier(output.String()), isSent: true}, nil
}

func printedIdentifier(output string) string {
	identifier := strings.TrimSpace(output)
	if _, err := strconv.ParseUint(identifier, 10, 32); err != nil {
		return ""
	}

	return identifier
}

func SendIfUnfocused(
	ctx context.Context,
	writeEscape EscapeWriter,
	isTerminalFocused func() bool,
	args Args,
	expiry Expiry,
) (Notification, error) {
	if err := validate(args); err != nil {
		return Notification{}, err
	}
	if isTerminalFocused != nil && isTerminalFocused() {
		return Notification{}, nil
	}

	return Send(ctx, writeEscape, args, expiry)
}

func run(
	ctx context.Context,
	writeEscape EscapeWriter,
	isTerminalFocused func() bool,
	args Args,
) (string, error) {
	notification, err := SendIfUnfocused(ctx, writeEscape, isTerminalFocused, args, ExpiresAsUsual)
	if err != nil {
		return "", err
	}
	if !notification.IsSent() {
		return focusedResult, nil
	}

	return "notification sent", nil
}
