package extensions

import (
	"fmt"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/commands"
)

type Context struct {
	CWD       string
	SessionID string
}

type ExtensionContext = Context

type Contribution struct {
	Tools         []agent.Tool
	SlashCommands []commands.SlashCommand
	Banner        string
}

type ExtensionContribution = Contribution

type Extension interface {
	Name() string
	Init(Context) (Contribution, error)
}

type AgentExtension = Extension

type DescribedExtension interface {
	Description() string
}

func Name(extension Extension) string {
	return extension.Name()
}

func Description(extension Extension) string {
	described, ok := extension.(DescribedExtension)
	if !ok {
		return ""
	}
	return described.Description()
}

func Init(extension Extension, ctx Context) (Contribution, error) {
	return extension.Init(ctx)
}

type Registry struct {
	extensions []Extension
}

type ExtensionRegistry = Registry

type InitOutput struct {
	Tools         []agent.Tool
	SlashCommands []commands.SlashCommand
	Commands      []commands.SlashCommand
	Banners       []string
	Errors        []string
}

func NewRegistry() *Registry {
	return &Registry{}
}

func Default() *Registry {
	return NewRegistry()
}

func NewExtensionRegistry() *ExtensionRegistry {
	return NewRegistry()
}

func (registry *Registry) Register(extension Extension) {
	registry.extensions = append(registry.extensions, extension)
}

func (registry *Registry) Extensions() []Extension {
	return append([]Extension(nil), registry.extensions...)
}

func (registry *Registry) Iter() []Extension {
	return registry.Extensions()
}

func (registry *Registry) InitAll(ctx Context) InitOutput {
	out := InitOutput{}
	for _, extension := range registry.extensions {
		contribution, err := initOne(extension, ctx)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		out.Tools = append(out.Tools, contribution.Tools...)
		out.SlashCommands = append(out.SlashCommands, contribution.SlashCommands...)
		out.Commands = append(out.Commands, contribution.SlashCommands...)
		if contribution.Banner != "" {
			out.Banners = append(out.Banners, fmt.Sprintf("%s: %s", extension.Name(), contribution.Banner))
		}
	}
	return out
}

func initOne(extension Extension, ctx Context) (contribution Contribution, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s: panicked during init", extension.Name())
		}
	}()
	contribution, err = extension.Init(ctx)
	if err != nil {
		return Contribution{}, fmt.Errorf("%s: %w", extension.Name(), err)
	}
	return contribution, nil
}
