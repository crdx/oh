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
	"unicode"
	"unicode/utf8"

	"crdx.org/oh/internal/app/slash"
)

const (
	snippetPrefix       = "//"
	helpCommandName     = "help"
	defaultFunctionName = "default"
	argumentFieldName   = "Arg"
	argumentsFieldName  = "Args"
	defaultPlaceholder  = "args"
	rootVariableName    = "$"
)

func New(configuredDefinitions map[string]Definition) (slash.CommandSet, error) {
	commands := make([]slash.Command, 0, len(configuredDefinitions))
	for _, name := range slices.Sorted(maps.Keys(configuredDefinitions)) {
		definition := configuredDefinitions[name]
		subject := definition.subject(name)
		prompt := strings.TrimSpace(definition.Prompt)
		if prompt == "" {
			return slash.CommandSet{}, fmt.Errorf("%s: prompt is empty", subject)
		}
		promptTemplate, err := template.New(name).
			Funcs(template.FuncMap{defaultFunctionName: defaultValue}).
			Option("missingkey=error").
			Parse(prompt)
		if err != nil {
			return slash.CommandSet{}, fmt.Errorf("%s: %w", subject, err)
		}

		argumentName, err := getArgumentName(promptTemplate.Tree)
		if err != nil {
			return slash.CommandSet{}, fmt.Errorf("%s: %w", subject, err)
		}
		argumentPolicy := definition.Arguments
		if argumentPolicy == "" {
			argumentPolicy = inferArgumentPolicyFromTemplate(promptTemplate.Tree)
		}
		if argumentPolicy != ArgumentsRequired && argumentPolicy != ArgumentsOptional && argumentPolicy != ArgumentsNone {
			return slash.CommandSet{}, fmt.Errorf("%s: invalid argument policy %q", subject, argumentPolicy)
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
				data := map[string]any{
					argumentFieldName:  arguments.Text,
					argumentsFieldName: arguments.Fields,
				}
				if argumentName != "" {
					data[argumentName] = arguments.Text
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
		placeholder := getPlaceholder(argumentName)
		switch argumentPolicy {
		case ArgumentsRequired:
			command = command.WithArgumentUsage("<" + placeholder + ">")
		case ArgumentsOptional:
			command = command.WithArgumentUsage("[<" + placeholder + ">]")
		case ArgumentsNone:
		}
		commands = append(commands, command)
	}

	var set slash.CommandSet
	var help slash.Command
	help = slash.Command{
		Name:        helpCommandName,
		Description: "list the configured snippets",
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

func getArgumentName(tree *parse.Tree) (string, error) {
	var names []string
	walkTemplate(tree.Root, true, func(node parse.Node, isRootDot bool) {
		name := getRootFieldName(node, isRootDot)
		if name != "" && name != argumentsFieldName && !slices.Contains(names, name) {
			names = append(names, name)
		}
	})
	switch len(names) {
	case 0:
		return "", nil
	case 1:
		if names[0] == argumentFieldName {
			return "", nil
		}
		return names[0], nil
	default:
		return "", fmt.Errorf("argument is named both %s and %s, want one name", names[0], names[1])
	}
}

func getRootFieldName(node parse.Node, isRootDot bool) string {
	switch typedNode := node.(type) {
	case *parse.FieldNode:
		if isRootDot {
			return typedNode.Ident[0]
		}
	case *parse.VariableNode:
		if len(typedNode.Ident) > 1 && typedNode.Ident[0] == rootVariableName {
			return typedNode.Ident[1]
		}
	}
	return ""
}

func getPlaceholder(argumentName string) string {
	if argumentName == "" {
		return defaultPlaceholder
	}
	var placeholder strings.Builder
	for i, character := range argumentName {
		if character == '_' {
			placeholder.WriteByte('-')
			continue
		}
		if i > 0 && unicode.IsUpper(character) {
			previous, _ := utf8.DecodeLastRuneInString(argumentName[:i])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) {
				placeholder.WriteByte('-')
			}
		}
		placeholder.WriteRune(unicode.ToLower(character))
	}
	return placeholder.String()
}

func inferArgumentPolicyFromTemplate(tree *parse.Tree) ArgumentPolicy {
	usesArguments := false
	usesDefault := false
	walkTemplate(tree.Root, true, func(node parse.Node, isRootDot bool) {
		usesArguments = usesArguments || getRootFieldName(node, isRootDot) != ""
		if identifier, ok := node.(*parse.IdentifierNode); ok {
			usesDefault = usesDefault || identifier.Ident == defaultFunctionName
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

func walkTemplate(node parse.Node, isRootDot bool, visit func(parse.Node, bool)) {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return
	}
	visit(node, isRootDot)

	switch typedNode := node.(type) {
	case *parse.ListNode:
		for _, child := range typedNode.Nodes {
			walkTemplate(child, isRootDot, visit)
		}
	case *parse.ActionNode:
		walkTemplate(typedNode.Pipe, isRootDot, visit)
	case *parse.PipeNode:
		for _, command := range typedNode.Cmds {
			walkTemplate(command, isRootDot, visit)
		}
	case *parse.CommandNode:
		for _, argument := range typedNode.Args {
			walkTemplate(argument, isRootDot, visit)
		}
	case *parse.IfNode:
		walkBranch(typedNode.BranchNode, isRootDot, isRootDot, visit)
	case *parse.RangeNode:
		walkBranch(typedNode.BranchNode, isRootDot, false, visit)
	case *parse.WithNode:
		walkBranch(typedNode.BranchNode, isRootDot, false, visit)
	case *parse.TemplateNode:
		walkTemplate(typedNode.Pipe, isRootDot, visit)
	case *parse.ChainNode:
		walkTemplate(typedNode.Node, isRootDot, visit)
	}
}

func walkBranch(branch parse.BranchNode, isRootDot bool, isBodyRootDot bool, visit func(parse.Node, bool)) {
	walkTemplate(branch.Pipe, isRootDot, visit)
	walkTemplate(branch.List, isBodyRootDot, visit)
	walkTemplate(branch.ElseList, isRootDot, visit)
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
