package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"strings"
	"time"
)

type Builder[T any] struct {
	definition          Definition
	revision            string
	compatibleRevisions map[string]struct{}
	render              Renderer[T]
	validate            Validator[T]
	decode              Decoder[T]

	parallel       bool
	readOnly       bool
	stateName      string
	restore        Restorer
	emphasis       func(rendering CallRendering) Emphasis
	emphasisSource func(args T, subject string) string
	timeLimit      func(args T) time.Duration
	isAllowed      func() bool
	withheld       error
	fallback       CallRendering
}

func Implement[T any](definition Definition, render Renderer[T]) Builder[T] {
	return Builder[T]{
		definition: definition,
		revision:   "1",
		render:     render,
	}
}

func (self Builder[T]) DefaultsTo(rendering CallRendering) Builder[T] {
	self.fallback = rendering
	return self
}

func (self Builder[T]) Validate(validate Validator[T]) Builder[T] {
	self.validate = validate
	return self
}

func (self Builder[T]) Decode(decode Decoder[T]) Builder[T] {
	self.decode = decode
	return self
}

func (self Builder[T]) Revision(revision string) Builder[T] {
	self.revision = revision
	return self
}

func (self Builder[T]) CompatibleWith(revisions ...string) Builder[T] {
	self.compatibleRevisions = make(map[string]struct{}, len(revisions))
	for _, revision := range revisions {
		self.compatibleRevisions[revision] = struct{}{}
	}
	return self
}

func (self Builder[T]) IsEmbarrassinglyParallel() Builder[T] {
	self.parallel = true
	return self
}

func (self Builder[T]) ChangesNothing() Builder[T] {
	self.readOnly = true
	return self
}

func (self Builder[T]) State(name string, restore Restorer) Builder[T] {
	self.stateName = name
	self.restore = restore

	return self
}

func (self Builder[T]) Syntax(language string) Builder[T] {
	self.emphasis = func(CallRendering) Emphasis {
		return Emphasis{Kind: EmphasisSyntax, Value: language}
	}
	self.emphasisSource = nil

	return self
}

func (self Builder[T]) SyntaxFrom(language string, source func(args T, subject string) string) Builder[T] {
	self = self.Syntax(language)
	self.emphasisSource = source
	return self
}

func (self Builder[T]) Focuses(pick func(CallRendering) string) Builder[T] {
	self.emphasis = func(rendering CallRendering) Emphasis {
		return Emphasis{Kind: EmphasisFocus, Value: pick(rendering)}
	}
	self.emphasisSource = nil

	return self
}

func (self Builder[T]) FocusPath() Builder[T] {
	return self.Focuses(func(rendering CallRendering) string {
		subject := rendering.Subject
		if subject == "" {
			return ""
		}

		return path.Base(subject)
	})
}

func (self Builder[T]) Requires(isAllowed func() bool, withheld error) Builder[T] {
	self.isAllowed = isAllowed
	self.withheld = withheld

	return self
}

func (self Builder[T]) TakesAtMost(limit func(args T) time.Duration) Builder[T] {
	self.timeLimit = limit
	return self
}

func (self Builder[T]) Run(execute ResultExecutor[T]) Tool {
	return self.build(execute)
}

func (self Builder[T]) Plain(execute Executor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (ToolCallResult, error) {
		output, err := execute(ctx, args)
		return ToolCallResult{Output: output}, err
	})
}

func (self Builder[T]) Exec(execute MetricsExecutor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (ToolCallResult, error) {
		output, metrics, err := execute(ctx, args)
		return ToolCallResult{Output: output, Metrics: metrics}, err
	})
}

func (self Builder[T]) guardAccess(execute ResultExecutor[T]) ResultExecutor[T] {
	if self.isAllowed == nil {
		return execute
	}

	return func(ctx context.Context, args T) (ToolCallResult, error) {
		if !self.isAllowed() {
			return ToolCallResult{}, self.withheld
		}

		return execute(ctx, args)
	}
}

func (self Builder[T]) decodeArguments(arguments string) (T, error) {
	var args T

	if self.decode != nil {
		return self.decode(arguments)
	}

	if _, err := self.definition.Schema.Decode(arguments); err != nil {
		return args, err
	}

	if text := strings.TrimSpace(arguments); text != "" {
		if err := json.Unmarshal([]byte(text), &args); err != nil {
			return args, fmt.Errorf("could not parse the arguments: %w", err)
		}
	}

	return args, nil
}

func (self Builder[T]) renderCall(args T) CallRendering {
	rendering := self.render(args)
	if self.emphasis != nil {
		rendering.Emphasis = self.emphasis(rendering)
	}
	if self.emphasisSource != nil {
		rendering.Emphasis.Source = self.emphasisSource(args, rendering.Subject)
	}
	return rendering
}

func (self Builder[T]) build(exec ResultExecutor[T]) Tool {
	exec = self.guardAccess(exec)

	return _tool{
		name:                self.definition.Name,
		description:         self.definition.Description,
		schema:              self.definition.Schema,
		revision:            self.revision,
		compatibleRevisions: maps.Clone(self.compatibleRevisions),
		parallel:            self.parallel,
		readOnly:            self.readOnly,
		stateName:           self.stateName,
		restore:             self.restore,
		fallback:            self.fallback,
		render: func(arguments string) (CallRendering, bool) {
			args, err := self.decodeArguments(arguments)
			if err != nil {
				return self.fallback, false
			}
			return self.renderCall(args), true
		},
		parse: func(arguments string) (_call, error) {
			args, err := self.decodeArguments(arguments)
			if err != nil {
				return _call{}, err
			}

			if self.validate != nil {
				if err := self.validate(args); err != nil {
					return _call{}, err
				}
			}

			rendering := self.renderCall(args)
			var timeLimit time.Duration
			if self.timeLimit != nil {
				timeLimit = self.timeLimit(args)
			}
			return _call{
				rendering: rendering,
				timeLimit: timeLimit,
				exec: func(ctx context.Context) (ToolCallResult, error) {
					return exec(ctx, args)
				},
			}, nil
		},
	}
}
