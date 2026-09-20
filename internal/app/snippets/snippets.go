package snippets

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"

	"crdx.org/oh/internal/app/slash"
)

const (
	snippetPrefix         = "//"
	helpCommandName       = "help"
	defaultFunctionName   = "default"
	requiredArgumentUsage = "<args>"
	optionalArgumentUsage = "[args]"
	argumentFieldName     = "Arg"
	argumentsFieldName    = "Args"
)

type templateData struct {
	Arg  string
	Args []string
}

func New(configuredDefinitions map[string]Definition) (slash.CommandSet, error) {
	commands := make([]slash.Command, 0, len(configuredDefinitions))
	for _, name := range slices.Sorted(maps.Keys(configuredDefinitions)) {
		definition := configuredDefinitions[name]
		prompt := strings.TrimSpace(definition.Prompt)
		if prompt == "" {
			return slash.CommandSet{}, fmt.Errorf("%s: prompt is empty", name)
		}
		promptTemplate, err := template.New(name).
			Funcs(template.FuncMap{defaultFunctionName: defaultValue}).
			Option("missingkey=error").
			Parse(prompt)
		if err != nil {
			return slash.CommandSet{}, fmt.Errorf("%s: %w", name, err)
		}

		argumentPolicy := definition.Arguments
		if argumentPolicy == "" {
			argumentPolicy = inferArgumentPolicyFromTemplate(promptTemplate.Tree)
		}
		if argumentPolicy != ArgumentsRequired && argumentPolicy != ArgumentsOptional && argumentPolicy != ArgumentsNone {
			return slash.CommandSet{}, fmt.Errorf("%s: invalid argument policy %q", name, argumentPolicy)
		}
		command := slash.Command{
			Name:        name,
			Description: definition.Description,
			Run: func(context slash.Context, arguments slash.Arguments) error {
				switch argumentPolicy {
				case ArgumentsRequired:
					if len(arguments.Fields) == 0 {
						return slash.Usage()
					}
				case ArgumentsNone:
					if len(arguments.Fields) != 0 {
						return slash.Usage()
					}
				case ArgumentsOptional:
				}

				var renderedText strings.Builder
				data := templateData{
					Arg:  arguments.Text,
					Args: arguments.Fields,
				}
				if err := promptTemplate.Execute(&renderedText, data); err != nil {
					return fmt.Errorf("could not render template: %w", err)
				}
				if strings.TrimSpace(renderedText.String()) == "" {
					return errors.New("template rendered an empty prompt")
				}
				context.Send(renderedText.String())
				return nil
			},
		}
		switch argumentPolicy {
		case ArgumentsRequired:
			command = command.WithArgumentUsage(requiredArgumentUsage)
		case ArgumentsOptional:
			command = command.WithArgumentUsage(optionalArgumentUsage)
		case ArgumentsNone:
		}
		commands = append(commands, command)
	}

	var set slash.CommandSet
	var help slash.Command
	help = slash.Command{
		Name: helpCommandName,
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			context.Notice(helpText(set.GetHelpEntries(), snippetPrefix+help.Name))
			return nil
		},
	}
	commands = append(commands, help)

	var err error
	set, err = slash.NewCommandSet(snippetPrefix, commands...)
	return set, err
}

func helpText(entries []slash.HelpEntry, hiddenUsage string) string {
	visible := slices.DeleteFunc(entries, func(entry slash.HelpEntry) bool { return entry.Usage == hiddenUsage })
	if len(visible) == 0 {
		return "No snippets are configured."
	}
	return "Snippets:\n" + strings.Join(slash.FormatHelp(visible), "\n")
}

func inferArgumentPolicyFromTemplate(tree *parse.Tree) ArgumentPolicy {
	usesArguments := false
	usesDefault := false
	walkTemplate(tree.Root, func(node parse.Node) {
		switch typedNode := node.(type) {
		case *parse.FieldNode:
			usesArguments = usesArguments || isArgumentField(typedNode.Ident)
		case *parse.IdentifierNode:
			usesDefault = usesDefault || typedNode.Ident == defaultFunctionName
		}
	})

	switch {
	case !usesArguments:
		return ArgumentsNone
	case usesDefault:
		return ArgumentsOptional
	default:
		return ArgumentsRequired
	}
}

func isArgumentField(identifiers []string) bool {
	return len(identifiers) > 0 &&
		(identifiers[0] == argumentFieldName || identifiers[0] == argumentsFieldName)
}

func walkTemplate(node parse.Node, visit func(parse.Node)) {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return
	}
	visit(node)

	switch typedNode := node.(type) {
	case *parse.ListNode:
		for _, child := range typedNode.Nodes {
			walkTemplate(child, visit)
		}
	case *parse.ActionNode:
		walkTemplate(typedNode.Pipe, visit)
	case *parse.PipeNode:
		for _, command := range typedNode.Cmds {
			walkTemplate(command, visit)
		}
	case *parse.CommandNode:
		for _, argument := range typedNode.Args {
			walkTemplate(argument, visit)
		}
	case *parse.IfNode:
		walkBranch(typedNode.BranchNode, visit)
	case *parse.RangeNode:
		walkBranch(typedNode.BranchNode, visit)
	case *parse.WithNode:
		walkBranch(typedNode.BranchNode, visit)
	case *parse.TemplateNode:
		walkTemplate(typedNode.Pipe, visit)
	case *parse.ChainNode:
		walkTemplate(typedNode.Node, visit)
	}
}

func walkBranch(branch parse.BranchNode, visit func(parse.Node)) {
	walkTemplate(branch.Pipe, visit)
	walkTemplate(branch.List, visit)
	walkTemplate(branch.ElseList, visit)
}

func defaultValue(fallback any, value any) any {
	if isEmpty(value) {
		return fallback
	}
	return value
}

func isEmpty(value any) bool {
	reflectedValue := reflect.ValueOf(value)
	if !reflectedValue.IsValid() {
		return true
	}

	switch reflectedValue.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return reflectedValue.Len() == 0
	case reflect.Bool:
		return !reflectedValue.Bool()
	case reflect.Complex64, reflect.Complex128:
		return reflectedValue.Complex() == 0
	case reflect.Chan, reflect.Func, reflect.Pointer, reflect.UnsafePointer, reflect.Interface:
		return reflectedValue.IsNil()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflectedValue.Int() == 0
	case reflect.Float32, reflect.Float64:
		return reflectedValue.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return reflectedValue.Uint() == 0
	case reflect.Invalid:
		return true
	case reflect.Struct:
		return false
	default:
		return false
	}
}
