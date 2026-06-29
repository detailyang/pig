package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/bugreport"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/cost"
	"github.com/detailyang/pig/goal"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/sessionexport"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/templates"
	"github.com/detailyang/pig/triggers"
)

var THINKING_LEVEL_VALUES = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

const THINKING_LEVEL_USAGE = "[off|minimal|low|medium|high|xhigh]"

type Sink func(string)

var consoleSink struct {
	sync.Mutex
	sink Sink
}

func SetSink(sink Sink) {
	consoleSink.Lock()
	defer consoleSink.Unlock()
	consoleSink.sink = sink
}

func ClearSink() {
	consoleSink.Lock()
	defer consoleSink.Unlock()
	consoleSink.sink = nil
}

func EmitLine(line string) {
	consoleSink.Lock()
	sink := consoleSink.sink
	consoleSink.Unlock()
	if sink != nil {
		sink(line)
		return
	}
	fmt.Println(line)
}

type Context struct {
	Harness            *harness.AgentHarness
	CWD                string
	SessionID          string
	SessionPath        string
	SessionName        *string
	Model              *ai.Model
	ToolCount          int
	LogPath            string
	Skills             []skills.Skill
	ThinkingLevel      ai.ThinkingLevel
	GoalState          *goal.State
	Cost               cost.Snapshot
	Templates          []templates.Template
	Branch             []session.Entry
	History            []string
	Sessions           []SessionListEntry
	TriggerRules       []triggers.DynamicRule
	TriggerSources     []TriggerSourceEntry
	TriggerRuntime     triggers.TriggerRuntimeSnapshot
	TriggerStoragePath string
	RunningTriggers    []RunningTriggerEntry
	CronJobs           []CronJobEntry
	Inbox              []triggers.InboxEntry
}

type CommandCtx = Context

type OutcomeKind string

const (
	OutcomeHandled                 OutcomeKind = "handled"
	OutcomeQuit                    OutcomeKind = "quit"
	OutcomeClearScreen             OutcomeKind = "clear_screen"
	OutcomeError                   OutcomeKind = "error"
	OutcomeAttachSkill             OutcomeKind = "attach_skill"
	OutcomeRunPrompt               OutcomeKind = "run_prompt"
	OutcomeSetThinkingLevel        OutcomeKind = "set_thinking_level"
	OutcomeOpenModelPicker         OutcomeKind = "open_model_picker"
	OutcomeSetModel                OutcomeKind = "set_model"
	OutcomeSetGoal                 OutcomeKind = "set_goal"
	OutcomeResetCost               OutcomeKind = "reset_cost"
	OutcomeRunPromptTemplate       OutcomeKind = "run_prompt_template"
	OutcomeExportSession           OutcomeKind = "export_session"
	OutcomeRunCompaction           OutcomeKind = "run_compaction"
	OutcomeMoveTo                  OutcomeKind = "move_to"
	OutcomeExportSessionArchive    OutcomeKind = "export_session_archive"
	OutcomeImportSessionArchive    OutcomeKind = "import_session_archive"
	OutcomeSessionImportActivation OutcomeKind = "session_import_activation"
	OutcomeReloadSkills            OutcomeKind = "reload_skills"
	OutcomeInstallSkill            OutcomeKind = "install_skill"
	OutcomeRemoveSkill             OutcomeKind = "remove_skill"
	OutcomeSetSkillState           OutcomeKind = "set_skill_state"
	OutcomeSetSessionName          OutcomeKind = "set_session_name"
	OutcomeWriteBugReport          OutcomeKind = "write_bug_report"
	OutcomeFindSessions            OutcomeKind = "find_sessions"
	OutcomeSetTriggerRuleEnabled   OutcomeKind = "set_trigger_rule_enabled"
	OutcomeRemoveTriggerRule       OutcomeKind = "remove_trigger_rule"
	OutcomeAbortTrigger            OutcomeKind = "abort_trigger"
	OutcomeAddTriggerRule          OutcomeKind = "add_trigger_rule"
	OutcomeAddCronJob              OutcomeKind = "add_cron_job"
	OutcomeSetCronJobEnabled       OutcomeKind = "set_cron_job_enabled"
	OutcomeRemoveCronJob           OutcomeKind = "remove_cron_job"
	OutcomeSetInboxStatus          OutcomeKind = "set_inbox_status"
	OutcomeClearInbox              OutcomeKind = "clear_inbox"
	OutcomeLoginSecret             OutcomeKind = "login_secret"
	OutcomeRemoveCredential        OutcomeKind = "remove_credential"
	OutcomeShareSession            OutcomeKind = "share_session"

	CommandOutcomeHandled                 = OutcomeHandled
	CommandOutcomeQuit                    = OutcomeQuit
	CommandOutcomeClearScreen             = OutcomeClearScreen
	CommandOutcomeAttachSkill             = OutcomeAttachSkill
	CommandOutcomeRunAgentPrompt          = OutcomeRunPrompt
	CommandOutcomeRunPromptTemplate       = OutcomeRunPromptTemplate
	CommandOutcomeRunCompaction           = OutcomeRunCompaction
	CommandOutcomeLoginSecret             = OutcomeLoginSecret
	CommandOutcomeOpenModelPicker         = OutcomeOpenModelPicker
	CommandOutcomeSessionImportActivation = OutcomeSessionImportActivation
)

type Outcome struct {
	Kind               OutcomeKind
	Message            string
	CWD                string
	Name               string
	Values             []string
	Prompt             string
	ErrorContext       string
	ThinkingLevel      ai.ThinkingLevel
	Model              ai.Model
	Goal               goal.State
	Vars               map[string]any
	Path               string
	Custom             string
	HasCustom          bool
	TargetID           *string
	SessionPath        string
	ExcludeTriggers    bool
	ActivateAutomation string
	Source             skills.Source
	Enabled            bool
	BugReport          bugreport.DiagInputs
	Query              string
	RemoveAll          bool
	Schedule           string
	Stateful           bool
	TriggerCondition   string
	TriggerAction      string
	FireOnce           bool
	PromoteToChat      bool
	InboxStatus        triggers.InboxStatus
	Provider           string
	StorageKey         string
	RecoveryCommand    string
	SessionID          string
	Description        string
	Public             bool
	Confirm            bool
	Overwrite          bool
	TriggerIDs         []string
	CronIDs            []string
}

type CommandOutcome = Outcome

func Handled() Outcome { return Outcome{Kind: OutcomeHandled} }

func HandledWithText(values []string) Outcome {
	return Outcome{Kind: OutcomeHandled, Values: append([]string(nil), values...)}
}

func Error(message string) Outcome { return Outcome{Kind: OutcomeError, Message: message} }

func Quit() Outcome { return Outcome{Kind: OutcomeQuit} }

func ClearScreen() Outcome { return Outcome{Kind: OutcomeClearScreen} }

func AttachSkill(name string) Outcome { return Outcome{Kind: OutcomeAttachSkill, Name: name} }

func RunPrompt(prompt, errorContext string) Outcome {
	return Outcome{Kind: OutcomeRunPrompt, Prompt: prompt, ErrorContext: errorContext}
}

func RunAgentPrompt(prompt, errorContext string) Outcome { return RunPrompt(prompt, errorContext) }

func RunPromptTemplate(name string, vars map[string]any) Outcome {
	return Outcome{Kind: OutcomeRunPromptTemplate, Name: name, Vars: copyVars(vars)}
}

func copyVars(vars map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range vars {
		out[key] = value
	}
	return out
}

func SetThinkingLevel(level ai.ThinkingLevel) Outcome {
	return Outcome{Kind: OutcomeSetThinkingLevel, ThinkingLevel: level, Message: fmt.Sprintf("thinking level: %s", level)}
}

func OpenModelPicker() Outcome { return Outcome{Kind: OutcomeOpenModelPicker} }

func SetModel(model ai.Model) Outcome {
	message := fmt.Sprintf("switched to %s:%s", model.Provider, model.ID)
	if hint := modelCredentialHint(string(model.Provider)); hint != "" {
		message = fmt.Sprintf("selected %s:%s, but login is required: %s", model.Provider, model.ID, hint)
	}
	return Outcome{Kind: OutcomeSetModel, Model: model, Message: message}
}

func SetGoal(state goal.State, message string) Outcome {
	return Outcome{Kind: OutcomeSetGoal, Goal: state, Message: message}
}

func ResetCost() Outcome { return Outcome{Kind: OutcomeResetCost, Message: "cost counters reset"} }

func ExportSession(path string) Outcome {
	return Outcome{Kind: OutcomeExportSession, Path: path, Message: "saved transcript: " + path}
}

func RunCompaction(custom string) Outcome {
	return Outcome{Kind: OutcomeRunCompaction, Custom: custom, HasCustom: true}
}

func RunCompactionDefault() Outcome {
	return Outcome{Kind: OutcomeRunCompaction}
}

func MoveTo(targetID *string) Outcome {
	return Outcome{Kind: OutcomeMoveTo, TargetID: cloneStringPtr(targetID), Message: "undid last turn"}
}

func ExportSessionArchive(sessionPath, outputPath string, excludeTriggers bool) Outcome {
	return Outcome{Kind: OutcomeExportSessionArchive, SessionPath: sessionPath, Path: outputPath, ExcludeTriggers: excludeTriggers, Message: sessionArchiveWarning()}
}

func ImportSessionArchive(path string) Outcome {
	return ImportSessionArchiveWithActivation(path, "off")
}

func ImportSessionArchiveWithActivation(path, activation string) Outcome {
	return Outcome{Kind: OutcomeImportSessionArchive, Path: path, ActivateAutomation: activation, Message: sessionArchiveWarning()}
}

func SessionImportActivation(sessionPath string, triggerIDs []string, cronIDs []string) Outcome {
	return Outcome{Kind: OutcomeSessionImportActivation, Path: sessionPath, TriggerIDs: append([]string(nil), triggerIDs...), CronIDs: append([]string(nil), cronIDs...)}
}

func ReloadSkills() Outcome { return Outcome{Kind: OutcomeReloadSkills, Message: "reload skills"} }

func InstallSkill(path string, confirm, overwrite bool) Outcome {
	return Outcome{Kind: OutcomeInstallSkill, Path: path, Confirm: confirm, Overwrite: overwrite, Message: "install skill: " + path}
}

func RemoveSkill(name string, source skills.Source, confirm bool) Outcome {
	return Outcome{Kind: OutcomeRemoveSkill, Name: name, Source: source, Confirm: confirm, Message: fmt.Sprintf("remove skill %s (%s)", name, source.Label())}
}

func SetSkillState(name string, source skills.Source, enabled bool) Outcome {
	return Outcome{Kind: OutcomeSetSkillState, Name: name, Source: source, Enabled: enabled, Message: fmt.Sprintf("set skill %s (%s) enabled=%t", name, source.Label(), enabled)}
}

func SetSessionName(name string) Outcome {
	return Outcome{Kind: OutcomeSetSessionName, Name: name, Message: "session name set to: " + name}
}

func WriteBugReport(diag bugreport.DiagInputs, path string) Outcome {
	return Outcome{Kind: OutcomeWriteBugReport, BugReport: diag, Path: path, Message: "write bug report: " + path}
}

func FindSessions(cwd, query string) Outcome {
	return Outcome{Kind: OutcomeFindSessions, CWD: cwd, Query: query, Message: "find sessions: " + query}
}

func SetTriggerRuleEnabled(id string, enabled bool) Outcome {
	verb := "disabled"
	if enabled {
		verb = "enabled"
	}
	return Outcome{Kind: OutcomeSetTriggerRuleEnabled, TargetID: outcomeStringPtr(id), Enabled: enabled, Message: fmt.Sprintf("%s trigger %s", verb, id)}
}

func RemoveTriggerRule(id string) Outcome {
	if id == "--all" {
		return Outcome{Kind: OutcomeRemoveTriggerRule, RemoveAll: true, Message: "remove all triggers"}
	}
	return Outcome{Kind: OutcomeRemoveTriggerRule, TargetID: outcomeStringPtr(id), Message: "remove trigger " + id}
}

func AbortTrigger(id string) Outcome {
	if id == "--all" {
		return Outcome{Kind: OutcomeAbortTrigger, RemoveAll: true, Message: "abort all triggers"}
	}
	return Outcome{Kind: OutcomeAbortTrigger, TargetID: outcomeStringPtr(id), Message: "abort trigger " + id}
}

func AddTriggerRule(condition, action string, fireOnce, promoteToChat bool) Outcome {
	return Outcome{Kind: OutcomeAddTriggerRule, TriggerCondition: condition, TriggerAction: action, FireOnce: fireOnce, PromoteToChat: promoteToChat, Message: fmt.Sprintf("add trigger rule: %s -> %s", condition, action)}
}

func AddCronJob(schedule, prompt string, stateful bool) Outcome {
	return Outcome{Kind: OutcomeAddCronJob, Schedule: schedule, Prompt: prompt, Stateful: stateful, Message: "add cron job: " + schedule}
}

func SetCronJobEnabled(id string, enabled bool) Outcome {
	verb := "disabled"
	if enabled {
		verb = "enabled"
	}
	return Outcome{Kind: OutcomeSetCronJobEnabled, TargetID: outcomeStringPtr(id), Enabled: enabled, Message: fmt.Sprintf("%s cron job %s", verb, id)}
}

func RemoveCronJob(id string) Outcome {
	return Outcome{Kind: OutcomeRemoveCronJob, TargetID: outcomeStringPtr(id), Message: "remove cron job " + id}
}

func SetInboxStatus(id string, status triggers.InboxStatus, message string) Outcome {
	return Outcome{Kind: OutcomeSetInboxStatus, TargetID: outcomeStringPtr(id), InboxStatus: status, Message: message}
}

func ClearInbox(count int) Outcome {
	entryWord := "entries"
	if count == 1 {
		entryWord = "entry"
	}
	return Outcome{Kind: OutcomeClearInbox, Message: fmt.Sprintf("dismissed %d inbox %s", count, entryWord)}
}

func LoginSecret(provider, storageKey, recoveryCommand string) Outcome {
	return Outcome{Kind: OutcomeLoginSecret, Provider: provider, StorageKey: storageKey, RecoveryCommand: recoveryCommand}
}

func RemoveCredential(provider string) Outcome {
	return Outcome{Kind: OutcomeRemoveCredential, Provider: provider, Message: fmt.Sprintf("remove credential for `%s`", provider)}
}

func ShareSession(sessionID, path string, public bool) Outcome {
	return Outcome{Kind: OutcomeShareSession, SessionID: sessionID, Path: path, Public: public, Description: "pie session " + sessionID}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func outcomeStringPtr(value string) *string { return &value }

type SlashCommand interface {
	Name() string
	Description() string
	Run(context.Context, []string, Context) Outcome
}

type AliasedCommand interface {
	Aliases() []string
}

type UsageCommand interface {
	Usage() string
}

type Registry struct {
	commands []SlashCommand
}

func NewRegistry() *Registry { return &Registry{} }

func (registry *Registry) Register(command SlashCommand) {
	registry.commands = append(registry.commands, command)
}

func (registry *Registry) Commands() []SlashCommand {
	return append([]SlashCommand(nil), registry.commands...)
}

func (registry *Registry) Find(name string) SlashCommand {
	for _, command := range registry.commands {
		if command.Name() == name {
			return command
		}
		aliased, ok := command.(AliasedCommand)
		if !ok {
			continue
		}
		for _, alias := range aliased.Aliases() {
			if alias == name {
				return command
			}
		}
	}
	return nil
}

func Parse(input string) (string, []string, bool) {
	trimmed := trimLeftSpace(input)
	if trimmed == "" || trimmed[0] != '/' {
		return "", nil, false
	}
	body := trimmed[1:]
	argv := splitCommandArgs(body)
	if len(argv) == 0 {
		return "", nil, false
	}
	return argv[0], append([]string(nil), argv[1:]...), true
}

func Dispatch(ctx context.Context, input string, registry *Registry, commandContext Context) Outcome {
	name, argv, ok := Parse(input)
	if !ok {
		return Error("not a slash command")
	}
	if name == "help" {
		return helpOutcome(registry, argv, commandContext.Skills)
	}
	command := registry.Find(name)
	if command == nil {
		if out, ok := runSkillShortcut(name, argv, registry, commandContext.Skills); ok {
			return out
		}
		return Error(fmt.Sprintf("unknown command: /%s (try /help)", name))
	}
	return command.Run(ctx, argv, commandContext)
}

func DefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(HelpCommand{})
	registry.Register(ClearCommand{})
	registry.Register(SkillsCommand{})
	registry.Register(SkillCommand{})
	registry.Register(QuitCommand{})
	registry.Register(ModelCommand{})
	registry.Register(ThinkingCommand{})
	registry.Register(CostCommand{})
	registry.Register(DiagCommand{})
	registry.Register(TemplateCommand{})
	registry.Register(SaveCommand{})
	registry.Register(CompactCommand{})
	registry.Register(UndoCommand{})
	registry.Register(BugReportCommand{})
	registry.Register(NameCommand{})
	registry.Register(SessionCommand{})
	registry.Register(SessionsCommand{})
	registry.Register(ShareCommand{})
	registry.Register(LoginCommand{})
	registry.Register(LogoutCommand{})
	registry.Register(FindCommand{})
	registry.Register(HistoryCommand{})
	registry.Register(GoalCommand{})
	registry.Register(GoalStartCommand{})
	registry.Register(TriggersCommand{})
	registry.Register(NewTriggerCommand{})
	registry.Register(CronCommand{})
	registry.Register(InboxCommand{})
	return registry
}

func WithBuiltins() *Registry {
	return DefaultRegistry()
}

func AttachSkillPrompt(text, skillName string) string {
	if skillName == "" {
		return text
	}
	return fmt.Sprintf("Before answering, invoke the Skill tool with name \"%s\" and use that skill's instructions for this turn.\n\nUser request:\n%s", skillName, text)
}

type HelpCommand struct{}

func (HelpCommand) Name() string        { return "help" }
func (HelpCommand) Description() string { return "show available commands and model catalog help" }
func (HelpCommand) Usage() string       { return "[models|<command>]" }
func (HelpCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return Error("help command must be dispatched with registry")
}

func helpOutcome(registry *Registry, argv []string, available []skills.Skill) Outcome {
	var builder strings.Builder
	if len(argv) > 0 {
		name := strings.TrimPrefix(argv[0], "/")
		if name == "models" {
			return Outcome{Kind: OutcomeHandled, Message: modelHelpCatalogText()}
		}
		command := registry.Find(name)
		if command == nil {
			if skill, err := resolveSkillShortcut(available, registry, name); err != nil {
				return Error(err.Error())
			} else if skill != nil {
				return Outcome{Kind: OutcomeHandled, Message: skillShortcutHelpText(*skill)}
			}
			return Error(unknownHelpTopicText(registry, available, name))
		}
		return Outcome{Kind: OutcomeHandled, Message: commandHelpText(command)}
	}
	builder.WriteString("\nCommands:\n")
	for _, command := range registry.Commands() {
		writeGeneralCommandHelp(&builder, command)
	}
	shortcuts := skillShortcuts(available, registry)
	if len(shortcuts) > 0 {
		builder.WriteString("\nSkill commands:\n")
		for _, shortcut := range shortcuts {
			description := ""
			if shortcut.Description != "" {
				description = " — " + shortcut.Description
			}
			fmt.Fprintf(&builder, "  %s [prompt]    use loaded skill (%s)%s\n", shortcut.Command, shortcut.Source.Label(), description)
		}
	}
	builder.WriteString("\nModels:\n")
	for _, line := range modelHelpSummaryLines() {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteString("\nAnything else is sent as a prompt to the agent.\n")
	return Outcome{Kind: OutcomeHandled, Message: builder.String()}
}

func PrintHelp(registry *Registry, topic string) string {
	return PrintHelpWithSkills(registry, topic, nil)
}

func PrintHelpWithSkills(registry *Registry, topic string, available []skills.Skill) string {
	var argv []string
	if strings.TrimSpace(topic) != "" {
		argv = []string{topic}
	}
	return helpOutcome(registry, argv, available).Message
}

func SaveAPIKey(provider string, token string) (string, error) {
	store, err := config.LoadDefaultAuthStore()
	if err != nil {
		return "", fmt.Errorf("load auth store: %w", err)
	}
	store.Set(provider, config.ProviderCredential{Type: config.CredentialAPIKey, Value: token})
	if err := store.Save(); err != nil {
		return "", fmt.Errorf("save auth store: %w", err)
	}
	return config.AuthPath(), nil
}

func SaveApiKey(provider string, token string) (string, error) { return SaveAPIKey(provider, token) }

func commandHelpText(command SlashCommand) string {
	usage := "/" + command.Name()
	if usageCommand, ok := command.(UsageCommand); ok && usageCommand.Usage() != "" {
		usage += " " + usageCommand.Usage()
	}
	lines := []string{usage, "  " + command.Description()}
	if aliased, ok := command.(AliasedCommand); ok && len(aliased.Aliases()) > 0 {
		aliases := make([]string, 0, len(aliased.Aliases()))
		for _, alias := range aliased.Aliases() {
			aliases = append(aliases, "/"+alias)
		}
		lines = append(lines, "  aliases: "+joinStrings(aliases, ", "))
	}
	if command.Name() == "help" {
		lines = append(lines, "  examples: /help model, /help /quit, /help models")
	} else {
		lines = append(lines, "  more: /help "+command.Name())
	}
	return joinStrings(lines, "\n")
}

func unknownHelpTopicText(registry *Registry, available []skills.Skill, topic string) string {
	suggestions := []string{}
	for _, command := range registry.Commands() {
		if strings.HasPrefix(command.Name(), topic) {
			suggestions = append(suggestions, "/"+command.Name())
			continue
		}
		aliased, ok := command.(AliasedCommand)
		if !ok {
			continue
		}
		for _, alias := range aliased.Aliases() {
			if alias == topic {
				suggestions = append(suggestions, "/"+command.Name())
				break
			}
		}
		if len(suggestions) == 5 {
			break
		}
	}
	if len(suggestions) < 5 {
		for _, shortcut := range skillShortcuts(available, registry) {
			if strings.HasPrefix(strings.TrimPrefix(shortcut.Command, "/"), topic) {
				suggestions = append(suggestions, shortcut.Command)
			}
			if len(suggestions) == 5 {
				break
			}
		}
	}
	if len(suggestions) == 0 {
		return fmt.Sprintf("unknown help topic: %s\nRun /help to list commands or /help models for the model catalog.", topic)
	}
	return fmt.Sprintf("unknown help topic: %s\nDid you mean %s?", topic, joinStrings(suggestions, ", "))
}

func writeCommandHelp(builder *strings.Builder, command SlashCommand) {
	builder.WriteString("/")
	builder.WriteString(command.Name())
	if usage, ok := command.(UsageCommand); ok && usage.Usage() != "" {
		builder.WriteString(" ")
		builder.WriteString(usage.Usage())
	}
	builder.WriteString(" — ")
	builder.WriteString(command.Description())
	builder.WriteString("\n")
}

func writeGeneralCommandHelp(builder *strings.Builder, command SlashCommand) {
	usage := ""
	if usageCommand, ok := command.(UsageCommand); ok && usageCommand.Usage() != "" {
		usage = " " + usageCommand.Usage()
	}
	aliases := ""
	if aliased, ok := command.(AliasedCommand); ok && len(aliased.Aliases()) > 0 {
		aliases = " (aliases: " + joinStrings(aliased.Aliases(), ", ") + ")"
	}
	fmt.Fprintf(builder, "  /%s%s    %s%s\n", command.Name(), usage, command.Description(), aliases)
}

type skillShortcut struct {
	Command     string
	Source      skills.Source
	Description string
}

type SkillShortcut = skillShortcut

func SkillShortcuts(available []skills.Skill, registry *Registry) []SkillShortcut {
	return skillShortcuts(available, registry)
}

func skillShortcuts(available []skills.Skill, registry *Registry) []skillShortcut {
	counts := map[string]int{}
	for _, skill := range available {
		if !skill.DisableModelInvocation {
			counts[skill.Name]++
		}
	}
	shortcuts := []skillShortcut{}
	for _, skill := range available {
		if skill.DisableModelInvocation || counts[skill.Name] != 1 || registry.Find(skill.Name) != nil {
			continue
		}
		shortcuts = append(shortcuts, skillShortcut{Command: "/" + skill.Name, Source: skill.Source, Description: previewText(skill.Description, 72)})
	}
	sort.Slice(shortcuts, func(i, j int) bool { return shortcuts[i].Command < shortcuts[j].Command })
	return shortcuts
}

func resolveSkillShortcut(available []skills.Skill, registry *Registry, name string) (*skills.Skill, error) {
	if registry.Find(name) != nil {
		return nil, nil
	}
	matches := []skills.Skill{}
	for _, skill := range available {
		if skill.Name == name {
			matches = append(matches, skill)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	enabled := []skills.Skill{}
	for _, skill := range matches {
		if !skill.DisableModelInvocation {
			enabled = append(enabled, skill)
		}
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("skill '%s' is disabled; run /skills enable %s [source] or /skills to list loaded skills", name, name)
	}
	if len(enabled) > 1 {
		return nil, fmt.Errorf("multiple enabled skills named '%s'; use /skill %s after resolving the source with /skills show %s [source]", name, name, name)
	}
	return &enabled[0], nil
}

func runSkillShortcut(name string, argv []string, registry *Registry, available []skills.Skill) (Outcome, bool) {
	skill, err := resolveSkillShortcut(available, registry, name)
	if err != nil {
		return Error(err.Error()), true
	}
	if skill == nil {
		return Outcome{}, false
	}
	if len(argv) == 0 {
		out := AttachSkill(skill.Name)
		out.Message = fmt.Sprintf("using skill: %s (%s) for next turn", skill.Name, skill.Source.Label())
		return out, true
	}
	return RunPrompt(AttachSkillPrompt(joinStrings(argv, " "), skill.Name), "skill command failed: "), true
}

func skillShortcutHelpText(skill skills.Skill) string {
	lines := []string{
		fmt.Sprintf("/%s [prompt]", skill.Name),
		fmt.Sprintf("  use loaded skill '%s' (%s)", skill.Name, skill.Source.Label()),
	}
	if skill.Description != "" {
		lines = append(lines, "  "+previewText(skill.Description, 120))
	}
	lines = append(lines, "  equivalent: /skill "+skill.Name)
	return joinStrings(lines, "\n")
}

type ClearCommand struct{}

func (ClearCommand) Name() string        { return "clear" }
func (ClearCommand) Description() string { return "clear screen (keeps conversation history)" }
func (ClearCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return ClearScreen()
}

type SkillCommand struct{}

func (SkillCommand) Name() string        { return "skill" }
func (SkillCommand) Description() string { return "attach a loaded skill to the next prompt" }
func (SkillCommand) Usage() string       { return "<name>" }
func (SkillCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) != 1 {
		return Error("usage: /skill <name>")
	}
	name := argv[0]
	for _, skill := range commandContext.Skills {
		if skill.Name != name {
			continue
		}
		if skill.DisableModelInvocation {
			return Error(fmt.Sprintf("skill '%s' is disabled (disable_model_invocation=true); edit the skill frontmatter to enable it", name))
		}
		out := AttachSkill(name)
		out.Message = fmt.Sprintf("using skill: %s (%s) for next turn", skill.Name, skill.Source.Label())
		return out
	}
	hint := skillHint(name, commandContext.Skills)
	return Error(fmt.Sprintf("no skill named '%s'. Run /skills to list loaded skills.%s", name, hint))
}

type SkillsCommand struct{}

func (SkillsCommand) Name() string { return "skills" }
func (SkillsCommand) Description() string {
	return "list, install, inspect, reload, enable, disable, or remove skills"
}
func (SkillsCommand) Usage() string {
	return "[install [--confirm] [--overwrite] <url|path>|show <name>|reload|enable <name> [source]|disable <name> [source]|remove [--confirm] <name> [source]]"
}
func (SkillsCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return Outcome{Kind: OutcomeHandled, Message: skillsListText(commandContext.Skills)}
	}
	switch argv[0] {
	case "list", "ls":
		return Outcome{Kind: OutcomeHandled, Message: skillsListText(commandContext.Skills)}
	case "install":
		return installSkillOutcome(argv[1:], commandContext.CWD)
	case "show":
		return showSkillOutcome(argv[1:], commandContext.Skills)
	case "reload":
		return ReloadSkills()
	case "enable":
		return setSkillStateOutcome(argv[1:], commandContext.Skills, true)
	case "disable":
		return setSkillStateOutcome(argv[1:], commandContext.Skills, false)
	case "remove":
		return removeSkillOutcome(argv[1:])
	default:
		return Error("usage: /skills " + SkillsCommand{}.Usage())
	}
}

func installSkillOutcome(argv []string, cwd string) Outcome {
	confirm := false
	overwrite := false
	positionals := []string{}
	for _, arg := range argv {
		switch {
		case arg == "--confirm" || arg == "--yes":
			confirm = true
		case arg == "--overwrite":
			overwrite = true
		case strings.HasPrefix(arg, "--"):
			return Error(fmt.Sprintf("unknown option for /skills install: %s", arg))
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) != 1 {
		return Error("usage: /skills install [--confirm] [--overwrite] <https-url|path>")
	}
	target := positionals[0]
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = resolveAgainstCWD(target, cwd)
	}
	return InstallSkill(target, confirm, overwrite)
}

func skillsListText(available []skills.Skill) string {
	if len(available) == 0 {
		return "(no skills loaded — drop SKILL.md files under ~/.pie/skills/<name>/ or <cwd>/.pie/skills/<name>/)"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Loaded skills (%d):\n", len(available))
	for _, skill := range available {
		fmt.Fprintf(&builder, "  - %s  (%s)", skill.Name, skill.Source.Label())
		if skill.DisableModelInvocation {
			builder.WriteString("  [disabled: disable_model_invocation=true]")
		}
		builder.WriteString("\n")
		if skill.Description != "" {
			fmt.Fprintf(&builder, "      %s\n", skill.Description)
		}
		fmt.Fprintf(&builder, "      path: %s\n", skill.FilePath)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func showSkillOutcome(argv []string, available []skills.Skill) Outcome {
	if len(argv) < 1 {
		return Error("usage: /skills show <name> [source]")
	}
	if len(argv) > 2 {
		argv = argv[:2]
	}
	source, hasSource, err := optionalSkillSource(argv)
	if err != nil {
		return Error(err.Error())
	}
	skill, err := resolveActiveSkill(available, argv[0], source, hasSource)
	if err != nil {
		return Error(err.Error())
	}
	status := "enabled"
	if skill.DisableModelInvocation {
		status = "disabled"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Skill: %s (%s)\n", skill.Name, skill.Source.Label())
	fmt.Fprintf(&builder, "Status: %s\n", status)
	if skill.Description != "" {
		fmt.Fprintf(&builder, "Description: %s\n", skill.Description)
	}
	fmt.Fprintf(&builder, "Path: %s\n", skill.FilePath)
	builder.WriteString("Body: not shown; use the file path if you need to inspect the full skill.")
	return Outcome{Kind: OutcomeHandled, Message: builder.String()}
}

func setSkillStateOutcome(argv []string, available []skills.Skill, enabled bool) Outcome {
	if len(argv) < 1 {
		verb := "enable"
		if !enabled {
			verb = "disable"
		}
		return Error(fmt.Sprintf("usage: /skills %s <name> [source]", verb))
	}
	if len(argv) > 2 {
		argv = argv[:2]
	}
	source, hasSource, err := optionalSkillSource(argv)
	if err != nil {
		return Error(err.Error())
	}
	skill, err := resolveActiveSkill(available, argv[0], source, hasSource)
	if err != nil {
		return Error(err.Error())
	}
	wasEnabled := !skill.DisableModelInvocation
	if wasEnabled == enabled {
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		return Outcome{Kind: OutcomeHandled, Message: fmt.Sprintf("skill already %s: %s (%s)", state, skill.Name, skill.Source.Label())}
	}
	return SetSkillState(skill.Name, skill.Source, enabled)
}

func removeSkillOutcome(argv []string) Outcome {
	confirm := false
	positionals := []string{}
	for _, arg := range argv {
		switch {
		case arg == "--confirm" || arg == "--yes":
			confirm = true
		case strings.HasPrefix(arg, "--"):
			return Error(fmt.Sprintf("unknown option for /skills remove: %s", arg))
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) < 1 || len(positionals) > 2 {
		return Error("usage: /skills remove [--confirm] <name> [source]")
	}
	source, _, err := optionalSkillSource(positionals)
	if err != nil {
		return Error(err.Error())
	}
	return RemoveSkill(positionals[0], source, confirm)
}

func optionalSkillSource(argv []string) (skills.Source, bool, error) {
	if len(argv) < 2 {
		return "", false, nil
	}
	source, ok := parseSkillSource(argv[1])
	if !ok {
		return "", false, fmt.Errorf("invalid skill source; expected one of: builtin, user, project")
	}
	return source, true, nil
}

func parseSkillSource(raw string) (skills.Source, bool) {
	source := skills.Source(raw)
	switch source {
	case skills.SourceBuiltin, skills.SourceUser, skills.SourceProject:
		return source, true
	default:
		return "", false
	}
}

func resolveActiveSkill(available []skills.Skill, name string, source skills.Source, hasSource bool) (skills.Skill, error) {
	matches := []skills.Skill{}
	for _, skill := range available {
		if skill.Name != name {
			continue
		}
		if hasSource && skill.Source != source {
			continue
		}
		matches = append(matches, skill)
	}
	if len(matches) == 0 {
		if hasSource {
			return skills.Skill{}, fmt.Errorf("no active %s skill named '%s'. Run /skills to list loaded skills.", source.Label(), name)
		}
		return skills.Skill{}, fmt.Errorf("no active skill named '%s'. Run /skills to list loaded skills.", name)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return skills.Skill{}, fmt.Errorf("multiple active skills named '%s'; pass source: builtin, user, or project", name)
}

type QuitCommand struct{}

func (QuitCommand) Name() string        { return "quit" }
func (QuitCommand) Aliases() []string   { return []string{"exit", "q"} }
func (QuitCommand) Description() string { return "exit the REPL" }
func (QuitCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return Quit()
}

type ModelCommand struct{}

func (ModelCommand) Name() string        { return "model" }
func (ModelCommand) Description() string { return "show or switch the active model" }
func (ModelCommand) Usage() string       { return "[provider:model-id|list [provider]]" }
func (ModelCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return OpenModelPicker()
	}
	if argv[0] == "list" || argv[0] == "ls" {
		provider := ""
		if len(argv) > 1 {
			provider = argv[1]
		}
		catalog, err := modelCatalogText(provider)
		if err != nil {
			return Error(err.Error())
		}
		return Outcome{Kind: OutcomeHandled, Message: catalog}
	}
	provider, id, ok := parseModelSpec(joinStrings(argv, " "))
	if !ok {
		return Error("expected provider:model-id (provider/model-id also works), e.g. /model anthropic:claude-haiku-4-5")
	}
	model, ok := ai.GetModel(ai.Provider(provider), id)
	if !ok {
		return Error(unknownModelError(provider, id))
	}
	return SetModel(model)
}

type ThinkingCommand struct{}

func (ThinkingCommand) Name() string        { return "thinking" }
func (ThinkingCommand) Description() string { return "show or set the thinking level" }
func (ThinkingCommand) Usage() string       { return THINKING_LEVEL_USAGE }
func (ThinkingCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		level := commandContext.ThinkingLevel
		if level == "" {
			return Outcome{Kind: OutcomeHandled, Message: "thinking level: ?"}
		}
		return Outcome{Kind: OutcomeHandled, Message: fmt.Sprintf("thinking level: %s", level)}
	}
	level, ok := parseThinkingLevel(argv[0])
	if !ok {
		return Error(fmt.Sprintf("invalid level: %s", argv[0]))
	}
	return SetThinkingLevel(level)
}

type GoalCommand struct{}

func (GoalCommand) Name() string { return "goal" }
func (GoalCommand) Description() string {
	return "set, view, pause, resume, or clear the session goal stop hook"
}
func (GoalCommand) Usage() string { return "[<condition>|start <prompt>|pause|resume|clear]" }
func (GoalCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return Outcome{Kind: OutcomeHandled, Message: goalStatusText(commandContext.GoalState)}
	}
	switch argv[0] {
	case "pause":
		if len(argv) == 1 {
			return pauseGoal(commandContext.GoalState)
		}
	case "resume":
		if len(argv) == 1 {
			return resumeGoal(commandContext.GoalState)
		}
	case "clear":
		if len(argv) == 1 {
			return clearGoal(commandContext.GoalState)
		}
	case "start":
		return goalStartOutcome(joinStrings(argv[1:], " "), commandContext.GoalState)
	}
	condition := strings.TrimSpace(joinStrings(argv, " "))
	if condition == "" {
		return Error("usage: /goal <condition>")
	}
	state := goal.NewState(condition, time.Now())
	return SetGoal(state, fmt.Sprintf("goal set: %s\ngoal will continue after each successful turn until transcript evidence satisfies the condition\nstart by sending a normal prompt, or run /goal-start <prompt>", state.Condition))
}

type GoalStartCommand struct{}

func (GoalStartCommand) Name() string { return "goal-start" }
func (GoalStartCommand) Description() string {
	return "start working on the active session goal with a prompt"
}
func (GoalStartCommand) Usage() string { return "<prompt>" }
func (GoalStartCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return goalStartOutcome(joinStrings(argv, " "), commandContext.GoalState)
}

type CostCommand struct{}

func (CostCommand) Name() string        { return "cost" }
func (CostCommand) Description() string { return "show running token / USD totals for this session" }
func (CostCommand) Usage() string       { return "[reset]" }
func (CostCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) > 0 && argv[0] == "reset" {
		return ResetCost()
	}
	return Outcome{Kind: OutcomeHandled, Message: cost.FullBreakdown(commandContext.Cost)}
}

type DiagCommand struct{}

func (DiagCommand) Name() string { return "diag" }
func (DiagCommand) Description() string {
	return "show diagnostic info (model, thinking, cost, log path)"
}
func (DiagCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return Outcome{Kind: OutcomeHandled, Message: diagnosticText(commandContext)}
}

type TemplateCommand struct{}

func (TemplateCommand) Name() string { return "template" }
func (TemplateCommand) Description() string {
	return "list templates, or run one with /template <name> [k=v ...]"
}
func (TemplateCommand) Usage() string { return "[name] [k=v ...]" }
func (TemplateCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return Outcome{Kind: OutcomeHandled, Message: templateListText(commandContext.Templates)}
	}
	vars := map[string]any{}
	for _, arg := range argv[1:] {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return Error(fmt.Sprintf("expected k=v argument; got: %s", arg))
		}
		vars[key] = value
	}
	return RunPromptTemplate(argv[0], vars)
}

type SaveCommand struct{}

func (SaveCommand) Name() string        { return "save" }
func (SaveCommand) Description() string { return "export session transcript to Markdown" }
func (SaveCommand) Usage() string       { return "[path]" }
func (SaveCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	dest := ""
	if len(argv) > 0 {
		dest = argv[0]
	} else {
		dest = sessionexport.DefaultExportPath(commandContext.SessionID)
	}
	if !filepath.IsAbs(dest) && commandContext.CWD != "" {
		dest = filepath.Join(commandContext.CWD, dest)
	}
	return ExportSession(dest)
}

type CompactCommand struct{}

func (CompactCommand) Name() string { return "compact" }
func (CompactCommand) Description() string {
	return "force a context compaction now (no-op when nothing to summarize)"
}
func (CompactCommand) Usage() string { return "[\"custom instructions\"]" }
func (CompactCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return RunCompactionDefault()
	}
	return RunCompaction(joinStrings(argv, " "))
}

type UndoCommand struct{}

func (UndoCommand) Name() string { return "undo" }
func (UndoCommand) Description() string {
	return "remove the most recent user+assistant turn from the active branch"
}
func (UndoCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	for index := len(commandContext.Branch) - 1; index >= 0; index-- {
		entry := commandContext.Branch[index]
		if entry.EntryType != session.EntryTypeMessage || entry.Message == nil || entry.Message.Kind != agent.MessageKindLLM || entry.Message.LLM == nil || entry.Message.LLM.Role != ai.RoleUser {
			continue
		}
		return MoveTo(entry.ParentID)
	}
	return Error("no user message to undo")
}

type BugReportCommand struct{}

func (BugReportCommand) Name() string { return "bug-report" }
func (BugReportCommand) Description() string {
	return "write a redacted diagnostic dump for issue attachment"
}
func (BugReportCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	model := ""
	if commandContext.Model != nil {
		model = fmt.Sprintf("%s:%s", commandContext.Model.Provider, commandContext.Model.ID)
	}
	thinking := string(commandContext.ThinkingLevel)
	if thinking == "" {
		thinking = "?"
	}
	diag := bugreport.DiagInputs{
		SessionID:   commandContext.SessionID,
		Model:       model,
		Thinking:    thinking,
		ToolCount:   commandContext.ToolCount,
		SkillCount:  len(commandContext.Skills),
		CostSummary: cost.OneLineSummary(commandContext.Cost),
		LogPath:     commandContext.LogPath,
	}
	return WriteBugReport(diag, bugreport.DefaultDest(time.Now()))
}

type NameCommand struct{}

func (NameCommand) Name() string        { return "name" }
func (NameCommand) Description() string { return "show or set the current session's name" }
func (NameCommand) Usage() string       { return "[slug]" }
func (NameCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		if commandContext.SessionName == nil || strings.TrimSpace(*commandContext.SessionName) == "" {
			return Outcome{Kind: OutcomeHandled, Message: "(unnamed session)"}
		}
		return Outcome{Kind: OutcomeHandled, Message: "session name: " + strings.TrimSpace(*commandContext.SessionName)}
	}
	name := strings.TrimSpace(joinStrings(argv, " "))
	if name == "" {
		return Error("empty name")
	}
	return SetSessionName(name)
}

type SessionCommand struct{}

func (SessionCommand) Name() string        { return "session" }
func (SessionCommand) Description() string { return "export/import replayable .piesession backups" }
func (SessionCommand) Usage() string       { return "export [path] [--exclude-triggers] | import <path>" }
func (SessionCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 {
		return Error("usage: /session export [path] [--exclude-triggers] | /session import <path>")
	}
	switch argv[0] {
	case "export":
		return sessionExportOutcome(argv[1:], commandContext)
	case "import":
		return sessionImportOutcome(argv[1:], commandContext)
	default:
		return Error(fmt.Sprintf("unknown /session subcommand: %s; use /session export [path] or /session import <path>", argv[0]))
	}
}

type FindCommand struct{}

func (FindCommand) Name() string { return "find" }
func (FindCommand) Description() string {
	return "search every session in this cwd for prompts/replies containing <query>"
}
func (FindCommand) Usage() string { return "<query>" }
func (FindCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	query := strings.TrimSpace(joinStrings(argv, " "))
	if query == "" {
		return Error("usage: /find <query>")
	}
	return FindSessions(commandContext.CWD, query)
}

type SessionsCommand struct{}

func (SessionsCommand) Name() string        { return "sessions" }
func (SessionsCommand) Description() string { return "list sessions for this cwd" }
func (SessionsCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	return Outcome{Kind: OutcomeHandled, Message: sessionsListText(commandContext.Sessions)}
}

type HistoryCommand struct{}

func (HistoryCommand) Name() string { return "history" }
func (HistoryCommand) Description() string {
	return "show recent submitted prompts from ~/.pie/history"
}
func (HistoryCommand) Usage() string { return "[N]" }
func (HistoryCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	limit := 20
	if len(argv) > 0 {
		parsed, ok := parsePositiveInt(argv[0])
		if ok {
			limit = parsed
		}
	}
	return Outcome{Kind: OutcomeHandled, Message: historyListText(commandContext.History, limit)}
}

type NewTriggerCommand struct{}

func (NewTriggerCommand) Name() string { return "new-trigger" }
func (NewTriggerCommand) Description() string {
	return "create a dynamic natural-language trigger rule"
}
func (NewTriggerCommand) Usage() string { return "<natural-language trigger request>" }
func (NewTriggerCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	spec := strings.TrimSpace(joinStrings(argv, " "))
	if spec == "" {
		return Error("usage: /new-trigger <natural-language trigger request>")
	}
	if parsed, err := triggers.ParseTriggerRule(spec); err == nil {
		return AddTriggerRule(parsed.Condition, parsed.Action, true, false)
	}
	return RunPrompt(newTriggerPrompt(spec), "create trigger: ")
}

type TriggersCommand struct{}

func (TriggersCommand) Name() string { return "triggers" }
func (TriggersCommand) Description() string {
	return "show trigger sources, rules, running actions, and recent audit"
}
func (TriggersCommand) Usage() string {
	return "[status|rules|sources|enable <id>|disable <id>|remove <id>|remove --all|running|audit [N]|abort <trace_id>|abort --all]"
}
func (TriggersCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	subcommand := "status"
	if len(argv) > 0 {
		subcommand = argv[0]
	}
	switch subcommand {
	case "status":
		return Outcome{Kind: OutcomeHandled, Message: triggerStatusText(commandContext)}
	case "rules":
		return Outcome{Kind: OutcomeHandled, Message: dynamicTriggerRulesText(commandContext.TriggerRules, len(commandContext.TriggerRules))}
	case "enable", "resume":
		return triggerEnableOutcome(argv[1:], true, "enable")
	case "disable", "pause":
		return triggerEnableOutcome(argv[1:], false, "disable")
	case "remove", "rm", "delete":
		return triggerRemoveOutcome(argv[1:])
	case "sources", "hooks":
		return Outcome{Kind: OutcomeHandled, Message: triggerSourcesText(commandContext.TriggerSources)}
	case "running":
		return Outcome{Kind: OutcomeHandled, Message: runningTriggersText(commandContext.RunningTriggers)}
	case "audit":
		return triggerAuditOutcome(argv[1:], commandContext.Branch)
	case "abort":
		return triggerAbortOutcome(argv[1:])
	default:
		return Error(fmt.Sprintf("unknown /triggers command: %s. usage: /triggers %s", subcommand, TriggersCommand{}.Usage()))
	}
}

type CronCommand struct{}

func (CronCommand) Name() string        { return "cron" }
func (CronCommand) Aliases() []string   { return []string{"crontab"} }
func (CronCommand) Description() string { return "manage local scheduled agent jobs" }
func (CronCommand) Usage() string {
	return "[list|add \"<5-field-cron>\" <prompt>|enable <id>|disable <id>|remove <id>]"
}
func (CronCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	subcommand := "list"
	if len(argv) > 0 {
		subcommand = argv[0]
	}
	switch subcommand {
	case "list", "ls", "status":
		return Outcome{Kind: OutcomeHandled, Message: cronJobsText(commandContext.CronJobs)}
	case "add":
		return cronAddOutcome(argv[1:])
	case "enable", "resume":
		return cronSetEnabledOutcome(argv[1:], true, "enable")
	case "disable", "pause":
		return cronSetEnabledOutcome(argv[1:], false, "disable")
	case "remove", "rm", "delete":
		return cronRemoveOutcome(argv[1:])
	default:
		return Error(fmt.Sprintf("unknown /cron command: %s. usage: /cron %s", subcommand, CronCommand{}.Usage()))
	}
}

type InboxCommand struct{}

func (InboxCommand) Name() string { return "inbox" }
func (InboxCommand) Description() string {
	return "triage findings from loops (stateful cron jobs)"
}
func (InboxCommand) Usage() string { return "[all|claim <id|n>|dismiss <id|n>|clear]" }
func (InboxCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	subcommand := "list"
	if len(argv) > 0 {
		subcommand = argv[0]
	}
	switch subcommand {
	case "list":
		return Outcome{Kind: OutcomeHandled, Message: inboxListText(newInboxEntries(commandContext.Inbox))}
	case "all":
		return Outcome{Kind: OutcomeHandled, Message: inboxAllText(commandContext.Inbox)}
	case "claim":
		entry, err := resolveInboxTarget(commandContext.Inbox, argv[1:])
		if err != nil {
			return Error(err.Error())
		}
		out := SetInboxStatus(entry.ID, triggers.InboxStatusClaimed, "")
		out.Prompt = fmt.Sprintf("A recurring loop (%s) reported this finding — investigate and address it:\n%s", entry.Source, entry.Text)
		out.ErrorContext = "inbox claim"
		return out
	case "dismiss":
		entry, err := resolveInboxTarget(commandContext.Inbox, argv[1:])
		if err != nil {
			return Error(err.Error())
		}
		return SetInboxStatus(entry.ID, triggers.InboxStatusDismissed, "dismissed: "+entry.Text)
	case "clear":
		return ClearInbox(len(newInboxEntries(commandContext.Inbox)))
	default:
		return Error(fmt.Sprintf("unknown /inbox subcommand: %s; usage: /inbox [all|claim <n>|dismiss <n>|clear]", subcommand))
	}
}

type LoginCommand struct{}

func (LoginCommand) Name() string { return "login" }
func (LoginCommand) Description() string {
	return "store an API key for a provider in ~/.pie/auth.json"
}
func (LoginCommand) Usage() string { return "<provider>" }
func (LoginCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) != 1 || strings.TrimSpace(argv[0]) == "" {
		return Error("usage: /login <provider>  (pie will prompt for the API key without echoing it)")
	}
	return LoginSecret(argv[0], "", "")
}

func LoginRequiresTtyMessage(provider, recoveryCommand string) string {
	command := recoveryCommand
	if command == "" {
		command = "/login " + provider
	}
	return fmt.Sprintf("/login requires an interactive terminal so the API key is not echoed; run pie in a TTY and use `%s`", command)
}

type LogoutCommand struct{}

func (LogoutCommand) Name() string        { return "logout" }
func (LogoutCommand) Description() string { return "remove a stored credential from ~/.pie/auth.json" }
func (LogoutCommand) Usage() string       { return "<provider>" }
func (LogoutCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error("usage: /logout <provider>")
	}
	return RemoveCredential(argv[0])
}

type ShareCommand struct{}

func (ShareCommand) Name() string { return "share" }
func (ShareCommand) Description() string {
	return "upload transcript as a private Gist via gh (requires `gh` on PATH)"
}
func (ShareCommand) Usage() string { return "[--public]" }
func (ShareCommand) Run(ctx context.Context, argv []string, commandContext Context) Outcome {
	public := false
	for _, arg := range argv {
		if arg == "--public" {
			public = true
		}
	}
	return ShareSession(commandContext.SessionID, shareTranscriptPath(commandContext.SessionID), public)
}

func parseThinkingLevel(raw string) (ai.ThinkingLevel, bool) {
	switch strings.ToLower(raw) {
	case string(ai.ThinkingOff):
		return ai.ThinkingOff, true
	case string(ai.ThinkingMinimal):
		return ai.ThinkingMinimal, true
	case string(ai.ThinkingLow):
		return ai.ThinkingLow, true
	case string(ai.ThinkingMedium):
		return ai.ThinkingMedium, true
	case string(ai.ThinkingHigh):
		return ai.ThinkingHigh, true
	case string(ai.ThinkingXHigh):
		return ai.ThinkingXHigh, true
	default:
		return "", false
	}
}

func sessionExportOutcome(argv []string, commandContext Context) Outcome {
	excludeTriggers := false
	pathArg := ""
	for _, arg := range argv {
		if arg == "--exclude-triggers" {
			excludeTriggers = true
			continue
		}
		if pathArg == "" {
			pathArg = arg
			continue
		}
		return Error("usage: /session export [path] [--exclude-triggers]")
	}
	if commandContext.SessionPath == "" {
		return Error("session metadata is missing transcript path")
	}
	outputPath := pathArg
	if outputPath == "" {
		outputPath = defaultSessionArchivePath(commandContext.CWD, commandContext.SessionID)
	}
	outputPath = resolveAgainstCWD(outputPath, commandContext.CWD)
	return ExportSessionArchive(commandContext.SessionPath, outputPath, excludeTriggers)
}

func sessionImportOutcome(argv []string, commandContext Context) Outcome {
	if len(argv) != 1 {
		return Error("usage: /session import <path>")
	}
	return ImportSessionArchive(resolveAgainstCWD(argv[0], commandContext.CWD))
}

func defaultSessionArchivePath(cwd, sessionID string) string {
	return session.DefaultExportPath(cwd, sessionID)
}

func resolveAgainstCWD(path, cwd string) string {
	if path == "" || filepath.IsAbs(path) || cwd == "" {
		return path
	}
	return filepath.Join(cwd, path)
}

func sessionArchiveWarning() string {
	return "warning: .piesession archives include transcript and tool history. They do not include separate auth stores, provider credentials or MCP config."
}

func triggerEnableOutcome(argv []string, enabled bool, verb string) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error(fmt.Sprintf("usage: /triggers %s <id>", verb))
	}
	return SetTriggerRuleEnabled(argv[0], enabled)
}

func triggerRemoveOutcome(argv []string) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error("usage: /triggers remove <id>|--all")
	}
	return RemoveTriggerRule(argv[0])
}

func triggerAbortOutcome(argv []string) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error("usage: /triggers abort <trace_id>|--all")
	}
	return AbortTrigger(argv[0])
}

func triggerAuditOutcome(argv []string, entries []session.Entry) Outcome {
	limit := 10
	if len(argv) > 0 {
		parsed, ok := parsePositiveInt(argv[0])
		if ok {
			limit = parsed
		}
	}
	return Outcome{Kind: OutcomeHandled, Message: triggerAuditText(collectTriggerAuditRows(entries, limit))}
}

type RunningTriggerEntry struct {
	TraceID       string
	SourceLabel   string
	EventLabel    string
	StartedAt     time.Time
	PromptPreview string
}

type TriggerSourceEntry struct {
	State              string
	Reason             string
	LastEventAt        time.Time
	LastError          string
	QueuedCount        int
	DroppedCount       int
	DedupedCount       int
	SubscriptionLabels []string
	RequiresAttention  string
}

func triggerSourcesText(sources []TriggerSourceEntry) string {
	if len(sources) == 0 {
		return "(no trigger sources registered)"
	}
	lines := []string{fmt.Sprintf("Trigger sources (%d):", len(sources))}
	for index, source := range sources {
		lastEvent := "never"
		if !source.LastEventAt.IsZero() {
			lastEvent = source.LastEventAt.UTC().Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("  - source #%d: %s queued=%d dropped=%d deduped=%d last_event=%s%s", index+1, renderTriggerSourceState(source), source.QueuedCount, source.DroppedCount, source.DedupedCount, lastEvent, renderRequiresAttention(source)))
		labels := "subscriptions: none"
		if len(source.SubscriptionLabels) > 0 {
			labels = "subscriptions: " + joinStrings(source.SubscriptionLabels, ", ")
		}
		lines = append(lines, "      "+labels)
		if source.LastError != "" {
			lines = append(lines, "      last error: "+previewRunes(source.LastError, 160))
		}
	}
	return joinStrings(lines, "\n")
}

func renderTriggerSourceState(source TriggerSourceEntry) string {
	switch source.State {
	case "disconnected", "auth_failed":
		if source.Reason != "" {
			return fmt.Sprintf("%s (%s)", source.State, previewRunes(source.Reason, 80))
		}
	}
	return source.State
}

func renderRequiresAttention(source TriggerSourceEntry) string {
	if source.RequiresAttention == "" {
		return ""
	}
	return "  attention: " + previewRunes(source.RequiresAttention, 120)
}

func runningTriggersText(running []RunningTriggerEntry) string {
	if len(running) == 0 {
		return "(no running triggers)"
	}
	lines := []string{fmt.Sprintf("Running triggers (%d):", len(running))}
	for _, trigger := range running {
		lines = append(lines, fmt.Sprintf("  - %s  %s / %s  since %s", trigger.TraceID, trigger.SourceLabel, trigger.EventLabel, trigger.StartedAt.UTC().Format(time.RFC3339)))
		lines = append(lines, "      prompt: "+previewRunes(trigger.PromptPreview, 120))
	}
	return joinStrings(lines, "\n")
}

type TriggerAuditRow struct {
	CustomType  string
	Timestamp   string
	TraceID     string
	State       string
	SourceLabel string
	EventLabel  string
	Summary     string
}

func collectTriggerAuditRows(entries []session.Entry, limit int) []TriggerAuditRow {
	rows := []TriggerAuditRow{}
	for index := len(entries) - 1; index >= 0 && len(rows) < limit; index-- {
		row, ok := triggerAuditRow(entries[index])
		if ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func triggerAuditRow(entry session.Entry) (TriggerAuditRow, bool) {
	if entry.EntryType != session.EntryTypeCustom {
		return TriggerAuditRow{}, false
	}
	if entry.CustomType != "trigger" && entry.CustomType != "trigger_result" && entry.CustomType != "trigger_promotion" {
		return TriggerAuditRow{}, false
	}
	data, ok := entry.Data.(map[string]any)
	if !ok {
		return TriggerAuditRow{}, false
	}
	row := TriggerAuditRow{CustomType: entry.CustomType, Timestamp: entry.Timestamp, TraceID: stringMapField(data, "trace_id"), SourceLabel: stringMapField(data, "source_label"), EventLabel: stringMapField(data, "event_label")}
	switch entry.CustomType {
	case "trigger":
		row.State = stringMapField(data, "state")
		if row.State == "" {
			row.State = "unknown"
		}
		row.Summary = triggerSummary(data)
	case "trigger_result":
		if success, ok := data["success"].(bool); ok && success {
			row.State = "completed"
		} else if ok {
			row.State = "failed"
		} else {
			row.State = "unknown"
		}
		row.Summary = firstNonEmpty(stringMapField(data, "summary"), stringMapField(data, "reason"))
	case "trigger_promotion":
		row.State = stringMapField(data, "state")
		if row.State == "" {
			row.State = "unknown"
		}
		if status := stringMapField(data, "redaction_status"); status != "" {
			row.Summary = "redaction_status=" + status
		}
	}
	return row, true
}

func triggerAuditText(rows []TriggerAuditRow) string {
	if len(rows) == 0 {
		return "(no trigger audit entries in this session)"
	}
	lines := []string{fmt.Sprintf("Recent trigger audit (%d):", len(rows))}
	for _, row := range rows {
		trace := firstNonEmpty(row.TraceID, "unknown-trace")
		source := firstNonEmpty(row.SourceLabel, "-")
		event := firstNonEmpty(row.EventLabel, "-")
		lines = append(lines, fmt.Sprintf("  - %s  %s/%s  trace=%s  %s / %s", row.Timestamp, row.CustomType, row.State, trace, source, event))
		if row.Summary != "" {
			lines = append(lines, "      "+previewRunes(row.Summary, 160))
		}
	}
	return joinStrings(lines, "\n")
}

func triggerSummary(data map[string]any) string {
	lines := []string{}
	if summary := stringMapField(data, "payload_summary"); summary != "" {
		lines = append(lines, summary)
	}
	if decision, ok := data["evaluator_decision"].(map[string]any); ok {
		for _, field := range []string{"outcome", "permission", "reason", "replacement_policy", "previous_trace_id", "hop_count"} {
			if value := decisionDetailValue(decision[field]); value != "" {
				label := field
				if field == "outcome" {
					label = "decision"
				}
				lines = append(lines, label+": "+value)
			}
		}
	}
	return joinStrings(lines, "\n      ")
}

func decisionDetailValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func stringMapField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func newTriggerPrompt(spec string) string {
	return "The user asked pie to create a dynamic trigger. Extract the trigger condition and action from the request, then call NewTrigger with structured condition and action fields. Dynamic triggers fire once by default; set fire_once=false only when the user explicitly asks for a repeating trigger. Trigger output is shown in the TUI and audit by default; set promote_to_chat=true only when the user explicitly asks for trigger results to enter the main chat context or be visible to future turns. Do not require a fixed syntax. If either the condition or action is missing, ask one concise clarification question instead of calling tools.\n\nUser request:\n" + spec
}

func triggerStatusText(commandContext Context) string {
	rules := commandContext.TriggerRules
	enabledCount := 0
	fireOnceCount := 0
	promoteCount := 0
	for _, rule := range rules {
		if rule.Enabled {
			enabledCount++
		}
		if rule.FireOnce {
			fireOnceCount++
		}
		if rule.PromoteToChat {
			promoteCount++
		}
	}
	disabledCount := len(rules) - enabledCount
	repeatCount := len(rules) - fireOnceCount
	lines := []string{
		"Trigger status:",
		fmt.Sprintf("  dynamic rules: %d total, %d enabled, %d disabled (%d fire_once, %d repeat, %d promote_to_chat)", len(rules), enabledCount, disabledCount, fireOnceCount, repeatCount, promoteCount),
		fmt.Sprintf("  local dynamic checker: %d registered, polls every %ds while enabled rules exist", dynamicCheckerCount(commandContext.TriggerSources), triggers.DynamicTriggerPollIntervalSecs()),
		fmt.Sprintf("  push trigger sources: %d configured source(s) feed server-pushed events into the same trigger runtime", notificationHookCount(commandContext.TriggerSources)),
		"  storage: " + firstNonEmpty(commandContext.TriggerStoragePath, "memory"),
		"  output: default is TUI + audit only; rules marked promote_to_chat also enter the main chat context",
		fmt.Sprintf("  engine: accepted=%d deduped=%d cycle_suppressed=%d recent_traces=%d dedup_entries=%d running=%d", commandContext.TriggerRuntime.AcceptedTotal, commandContext.TriggerRuntime.DedupedTotal, commandContext.TriggerRuntime.CycleSuppressedTotal, commandContext.TriggerRuntime.ActiveTraces, commandContext.TriggerRuntime.DedupEntries, len(commandContext.RunningTriggers)),
		fmt.Sprintf("  sources: %d total, %d connected, %d require attention", len(commandContext.TriggerSources), connectedSourceCount(commandContext.TriggerSources), attentionSourceCount(commandContext.TriggerSources)),
	}
	if len(rules) > 0 {
		ruleLines := strings.Split(dynamicTriggerRulesText(rules, 3), "\n")
		if len(ruleLines) > 1 {
			lines = append(lines, ruleLines[1:]...)
		}
	}
	lines = append(lines, "  commands: /triggers rules | /triggers sources | /triggers disable <id> | /triggers enable <id> | /triggers remove <id> | /triggers audit")
	return joinStrings(lines, "\n")
}

func RenderTriggerStatus(commandContext Context) string {
	return triggerStatusText(commandContext)
}

func RenderTriggersStatus(commandContext Context) string {
	return triggerStatusText(commandContext)
}

func dynamicCheckerCount(sources []TriggerSourceEntry) int {
	count := 0
	for _, source := range sources {
		for _, label := range source.SubscriptionLabels {
			if strings.Contains(label, "dynamic trigger periodic check") {
				count++
				break
			}
		}
	}
	return count
}

func notificationHookCount(sources []TriggerSourceEntry) int {
	count := len(sources) - dynamicCheckerCount(sources)
	if count < 0 {
		return 0
	}
	return count
}

func connectedSourceCount(sources []TriggerSourceEntry) int {
	count := 0
	for _, source := range sources {
		if source.State == "connected" {
			count++
		}
	}
	return count
}

func attentionSourceCount(sources []TriggerSourceEntry) int {
	count := 0
	for _, source := range sources {
		if source.RequiresAttention != "" {
			count++
		}
	}
	return count
}

func dynamicTriggerRulesText(rules []triggers.DynamicRule, limit int) string {
	if len(rules) == 0 {
		return "Dynamic trigger rules: none"
	}
	shown := len(rules)
	if limit < shown {
		shown = limit
	}
	lines := []string{fmt.Sprintf("Dynamic trigger rules (%d):", len(rules))}
	for _, rule := range rules[:shown] {
		state := "disabled"
		if rule.Enabled {
			state = "enabled"
		}
		fireMode := "repeat"
		if rule.FireOnce {
			fireMode = "fire_once"
		}
		outputMode := "audit_only"
		if rule.PromoteToChat {
			outputMode = "promote_to_chat"
		}
		firedAt := ""
		if rule.FiredAt != nil {
			firedAt = ", fired_at=" + rule.FiredAt.UTC().Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("  - %s [%s, %s, %s%s] when %s -> %s", rule.ID, state, fireMode, outputMode, firedAt, previewRunes(rule.Condition, 80), previewRunes(rule.Action, 80)))
	}
	if shown < len(rules) {
		lines = append(lines, fmt.Sprintf("  ... %d more; run /triggers rules", len(rules)-shown))
	}
	return joinStrings(lines, "\n")
}

func RenderDynamicTriggerRules(rules []triggers.DynamicRule, limit int) string {
	return dynamicTriggerRulesText(rules, limit)
}

func previewRunes(value string, limit int) string {
	if runeLen(value) <= limit {
		return value
	}
	return truncateRunes(value, limit) + "…"
}

type CronJobEntry struct {
	ID                  string
	Schedule            string
	Action              string
	Enabled             bool
	Stateful            bool
	RunningTraceID      string
	LastFiredAt         *time.Time
	LastError           string
	SkippedOverlapCount uint64
}

func cronJobsText(jobs []CronJobEntry) string {
	if len(jobs) == 0 {
		return "Cron jobs (session): none"
	}
	lines := []string{fmt.Sprintf("Cron jobs (session, %d):", len(jobs))}
	for _, job := range jobs {
		state := "disabled"
		if job.Enabled {
			state = "enabled"
		}
		stateful := ""
		if job.Stateful {
			stateful = "  [stateful]"
		}
		running := ""
		if job.RunningTraceID != "" {
			running = ", running " + job.RunningTraceID
		}
		lines = append(lines, fmt.Sprintf("  %s  %s  %s%s%s", job.ID, state, job.Schedule, stateful, running))
		lines = append(lines, "    action: "+previewRunes(bugreport.Redact(job.Action), 120))
		if job.SkippedOverlapCount > 0 {
			lines = append(lines, fmt.Sprintf("    overlap skips: %d", job.SkippedOverlapCount))
		}
		if job.LastError != "" {
			lines = append(lines, "    last: "+job.LastError)
		} else if job.LastFiredAt != nil {
			lines = append(lines, "    last fired: "+job.LastFiredAt.UTC().Format(time.RFC3339))
		}
	}
	return joinStrings(lines, "\n")
}

func RenderCronJobs(jobs []CronJobEntry) string {
	return cronJobsText(jobs)
}

func cronAddOutcome(argv []string) Outcome {
	stateful := false
	filtered := make([]string, 0, len(argv))
	for _, arg := range argv {
		if arg == "--stateful" {
			stateful = true
			continue
		}
		filtered = append(filtered, arg)
	}
	if len(filtered) < 2 || !looksLikeFiveFieldCron(filtered[0]) {
		return Error("usage: /cron add [--stateful] \"<minute hour dom month dow>\" <prompt>")
	}
	prompt := strings.TrimSpace(joinStrings(filtered[1:], " "))
	if prompt == "" {
		return Error("usage: /cron add [--stateful] \"<minute hour dom month dow>\" <prompt>")
	}
	return AddCronJob(filtered[0], prompt, stateful)
}

func cronSetEnabledOutcome(argv []string, enabled bool, verb string) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error(fmt.Sprintf("usage: /cron %s <id>", verb))
	}
	return SetCronJobEnabled(argv[0], enabled)
}

func cronRemoveOutcome(argv []string) Outcome {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Error("usage: /cron remove <id>")
	}
	return RemoveCronJob(argv[0])
}

func looksLikeFiveFieldCron(value string) bool {
	return len(strings.Fields(value)) == 5
}

func shareTranscriptPath(sessionID string) string {
	if sessionID == "" {
		sessionID = "session"
	}
	return filepath.Join(os.TempDir(), "pie-share-"+sessionID, "transcript.md")
}

func newInboxEntries(entries []triggers.InboxEntry) []triggers.InboxEntry {
	var out []triggers.InboxEntry
	for _, entry := range entries {
		if entry.Status == triggers.InboxStatusNew {
			out = append(out, entry)
		}
	}
	return out
}

func inboxListText(entries []triggers.InboxEntry) string {
	if len(entries) == 0 {
		return "inbox: empty — stateful loops (/cron add --stateful) report findings here"
	}
	lines := []string{fmt.Sprintf("Inbox (%d new):", len(entries))}
	for index, entry := range entries {
		lines = append(lines, fmt.Sprintf("  %d. [%s] %s  (%s, %s)", index+1, truncateRunes(entry.ID, 12), entry.Text, entry.Source, truncateRunes(entry.CreatedAt, 16)))
	}
	lines = append(lines, "claim with /inbox claim <n>, dismiss with /inbox dismiss <n>")
	return joinStrings(lines, "\n")
}

func inboxAllText(entries []triggers.InboxEntry) string {
	lines := []string{fmt.Sprintf("Inbox history (%d total):", len(entries))}
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("  [%s] %s  (%s)", entry.Status, entry.Text, entry.Source))
	}
	return joinStrings(lines, "\n")
}

func resolveInboxTarget(entries []triggers.InboxEntry, argv []string) (triggers.InboxEntry, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return triggers.InboxEntry{}, fmt.Errorf("usage: /inbox claim|dismiss <n or inb-id>")
	}
	newEntries := newInboxEntries(entries)
	target := argv[0]
	if index, ok := parsePositiveInt(target); ok {
		lookupIndex := index
		if lookupIndex == 0 {
			lookupIndex = 1
		}
		if lookupIndex > len(newEntries) {
			return triggers.InboxEntry{}, fmt.Errorf("no inbox entry #%d (have %d)", index, len(newEntries))
		}
		return newEntries[lookupIndex-1], nil
	}
	for _, entry := range newEntries {
		if strings.HasPrefix(entry.ID, target) {
			return entry, nil
		}
	}
	return triggers.InboxEntry{}, fmt.Errorf("no new inbox entry matching '%s'", target)
}

type SessionListEntry struct {
	ID        string
	CreatedAt string
	Preview   string
}

type SessionSummaryInput struct {
	ID        string
	CreatedAt string
	Entries   []session.Entry
}

func SessionListEntriesFromSessions(sessions []SessionSummaryInput) []SessionListEntry {
	entries := make([]SessionListEntry, 0, len(sessions))
	for _, input := range sessions {
		entries = append(entries, SessionListEntry{ID: input.ID, CreatedAt: input.CreatedAt, Preview: firstSessionPreview(input.Entries)})
	}
	return entries
}

func sessionsListText(entries []SessionListEntry) string {
	if len(entries) == 0 {
		return "(no sessions for this cwd)"
	}
	var builder strings.Builder
	builder.WriteString("Sessions:\n")
	for _, entry := range entries {
		fmt.Fprintf(&builder, "  %s  %s  %s", truncateRunes(entry.ID, 16), entry.CreatedAt, entry.Preview)
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func firstSessionPreview(entries []session.Entry) string {
	for _, entry := range entries {
		text, ok := searchableEntryText(entry)
		if ok && strings.TrimSpace(text) != "" {
			return strings.ReplaceAll(truncateRunes(text, 120), "\n", " ")
		}
	}
	return ""
}

type SessionSearchInput struct {
	Path    string
	Entries []session.Entry
}

type FindMatch struct {
	Session string
	Snippet string
}

func FindMatches(query string, sessions []SessionSearchInput) []FindMatch {
	normalizedQuery := strings.ToLower(query)
	matches := []FindMatch{}
	for _, input := range sessions {
		sessionName := sessionStem(input.Path)
		for _, entry := range input.Entries {
			text, ok := searchableEntryText(entry)
			if !ok || !strings.Contains(strings.ToLower(text), normalizedQuery) {
				continue
			}
			snippet := strings.ReplaceAll(truncateRunes(text, 120), "\n", " ")
			matches = append(matches, FindMatch{Session: sessionName, Snippet: snippet})
		}
	}
	return matches
}

func FindResultsText(matches []FindMatch) string {
	if len(matches) == 0 {
		return "(no matches)"
	}
	var builder strings.Builder
	for _, match := range matches {
		fmt.Fprintf(&builder, "  %s  %s\n", match.Session, strings.ReplaceAll(match.Snippet, "\n", " "))
	}
	fmt.Fprintf(&builder, "(%d match(es))", len(matches))
	return builder.String()
}

func searchableEntryText(entry session.Entry) (string, bool) {
	if entry.EntryType != session.EntryTypeMessage || entry.Message == nil || entry.Message.Kind != agent.MessageKindLLM || entry.Message.LLM == nil {
		return "", false
	}
	switch entry.Message.LLM.Role {
	case ai.RoleUser, ai.RoleAssistant:
		return textBlocks(entry.Message.LLM.Content), true
	default:
		return "", false
	}
}

func textBlocks(blocks []ai.ContentBlock) string {
	texts := []string{}
	for _, block := range blocks {
		if block.Type == ai.ContentText {
			texts = append(texts, block.Text)
		}
	}
	return joinStrings(texts, " ")
}

func sessionStem(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if stem == "" || stem == "." {
		return "?"
	}
	return stem
}

func historyListText(entries []string, limit int) string {
	if len(entries) == 0 {
		return "(no history yet)"
	}
	start := len(entries) - limit
	if start < 0 {
		start = 0
	}
	var builder strings.Builder
	for index, entry := range entries[start:] {
		entryNumber := start + index + 1
		preview := truncateRunes(entry, 200)
		suffix := ""
		if runeLen(entry) > 200 {
			suffix = "…"
		}
		fmt.Fprintf(&builder, "  %d: %s%s\n", entryNumber, preview, suffix)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func parsePositiveInt(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	out := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
		out = out*10 + int(char-'0')
	}
	return out, true
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func runeLen(value string) int { return len([]rune(value)) }

func diagnosticText(commandContext Context) string {
	model := "(none)"
	if commandContext.Model != nil {
		model = fmt.Sprintf("%s:%s", commandContext.Model.Provider, commandContext.Model.ID)
	}
	thinking := "?"
	if commandContext.ThinkingLevel != "" {
		thinking = string(commandContext.ThinkingLevel)
	}
	logPath := commandContext.LogPath
	if logPath == "" {
		logPath = "(logging disabled)"
	}
	return fmt.Sprintf("\nDiagnostic snapshot:\n  session       %s\n  model         %s\n  thinking      %s\n  tools         %d\n  skills        %d\n  cost          %s\n  log file      %s\n", commandContext.SessionID, model, thinking, commandContext.ToolCount, len(commandContext.Skills), cost.OneLineSummary(commandContext.Cost), logPath)
}

func templateListText(available []templates.Template) string {
	if len(available) == 0 {
		return "(no templates loaded — drop `.md` files under ~/.pie/templates/ or <cwd>/.pie/templates/)"
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Loaded templates (%d):\n", len(available)))
	for _, template := range available {
		builder.WriteString(fmt.Sprintf("  /template %s  %s\n", template.Name, template.Description))
	}
	return builder.String()
}

func goalStatusText(state *goal.State) string {
	if state == nil || (!state.Active() && state.Status != goal.StatusAchieved) {
		return "no active goal; set one with /goal <condition>"
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("goal: %s\n", state.Condition))
	builder.WriteString(fmt.Sprintf("status: %s\n", state.Status))
	builder.WriteString(fmt.Sprintf("iterations: %d", state.Iterations))
	if state.LastReason != nil && *state.LastReason != "" {
		builder.WriteString(fmt.Sprintf("\nlast evaluator reason: %s", previewText(*state.LastReason, 240)))
	}
	return builder.String()
}

func pauseGoal(state *goal.State) Outcome {
	if state == nil {
		return Error("no active goal; set one with /goal <condition>")
	}
	paused, err := goal.Pause(*state, "paused by command", time.Now())
	if err != nil {
		return Error(err.Error())
	}
	return SetGoal(paused, fmt.Sprintf("goal paused: %s", paused.Condition))
}

func resumeGoal(state *goal.State) Outcome {
	if state == nil {
		return Error("no active goal; set one with /goal <condition>")
	}
	resumed, err := goal.Resume(*state, time.Now())
	if err != nil {
		return Error(err.Error())
	}
	return SetGoal(resumed, fmt.Sprintf("goal resumed: %s", resumed.Condition))
}

func clearGoal(state *goal.State) Outcome {
	if state == nil {
		return Error("no active goal; set one with /goal <condition>")
	}
	return SetGoal(goal.Clear(*state, time.Now()), "goal cleared")
}

func goalStartOutcome(prompt string, state *goal.State) Outcome {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Error("usage: /goal-start <prompt>")
	}
	if state == nil || !state.Active() {
		return Error("no active goal; set one with /goal <condition>")
	}
	return RunPrompt(prompt, "goal start: ")
}

func previewText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func parseModelSpec(spec string) (string, string, bool) {
	spec = strings.TrimSpace(spec)
	for _, separator := range []string{":", "/"} {
		if provider, id, ok := cutNonEmpty(spec, separator); ok {
			return provider, id, true
		}
	}
	for index, char := range spec {
		if char == ' ' || char == '\t' {
			provider := strings.TrimSpace(spec[:index])
			id := strings.TrimSpace(spec[index:])
			if provider != "" && id != "" {
				return provider, id, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

func ParseModelSpec(spec string) (string, string, bool) {
	return parseModelSpec(spec)
}

func cutNonEmpty(value, separator string) (string, string, bool) {
	left, right, ok := strings.Cut(value, separator)
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if !ok || left == "" || right == "" {
		return "", "", false
	}
	return left, right, true
}

func modelCatalogText(provider string) (string, error) {
	models, providers, counts := sortedModelsByProvider()
	var builder strings.Builder
	if provider == "" {
		fmt.Fprintf(&builder, "Supported providers/models: %d providers, %d models\n", len(providers), len(models))
		builder.WriteString("Custom models can be registered explicitly with config.LoadModelsFile/LoadModelsFiles; local models.json auto-loading is disabled.")
		for _, currentProvider := range providers {
			fmt.Fprintf(&builder, "\n  %s (%d)", currentProvider, counts[currentProvider])
			appendModelCatalogLines(&builder, models, currentProvider)
		}
		return builder.String(), nil
	}
	if _, ok := counts[provider]; !ok {
		return "", fmt.Errorf("unknown provider '%s'. Known providers: %s", provider, providerSummary(providers, counts))
	}
	fmt.Fprintf(&builder, "Supported models for provider '%s' (%d):", provider, counts[provider])
	appendModelCatalogLines(&builder, models, provider)
	return builder.String(), nil
}

func appendModelCatalogLines(builder *strings.Builder, models []ai.Model, provider string) {
	for _, model := range models {
		if string(model.Provider) != provider {
			continue
		}
		if strings.TrimSpace(model.Name) == "" || model.Name == model.ID {
			fmt.Fprintf(builder, "\n    - %s", model.ID)
		} else {
			fmt.Fprintf(builder, "\n    - %s — %s", model.ID, model.Name)
		}
	}
}

func modelHelpCatalogText() string {
	models, providers, counts := sortedModelsByProvider()
	var builder strings.Builder
	fmt.Fprintf(&builder, "Supported providers/models: %d providers, %d models\n", len(providers), len(models))
	builder.WriteString("Custom models can be registered explicitly with config.LoadModelsFile/LoadModelsFiles; local models.json auto-loading is disabled.")
	for _, provider := range providers {
		fmt.Fprintf(&builder, "\n  %s (%d)", provider, counts[provider])
		for _, model := range models {
			if string(model.Provider) != provider {
				continue
			}
			if strings.TrimSpace(model.Name) == "" || model.Name == model.ID {
				fmt.Fprintf(&builder, "\n    - %s", model.ID)
			} else {
				fmt.Fprintf(&builder, "\n    - %s — %s", model.ID, model.Name)
			}
		}
	}
	return builder.String()
}

func CLIModelHelpText() string {
	var builder strings.Builder
	builder.WriteString("Model catalog:\n")
	for _, line := range modelHelpSummaryLines() {
		builder.WriteString("  ")
		builder.WriteString(strings.TrimLeftFunc(line, unicode.IsSpace))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func CliModelHelpText() string { return CLIModelHelpText() }

func modelHelpSummaryLines() []string {
	models, providers, counts := sortedModelsByProvider()
	providerSummaries := make([]string, 0, len(providers))
	for _, provider := range providers {
		providerSummaries = append(providerSummaries, fmt.Sprintf("%s(%d)", provider, counts[provider]))
	}
	return []string{
		fmt.Sprintf("  Supported providers (%d), models (%d): %s", len(providers), len(models), joinStrings(providerSummaries, ", ")),
		"  Full list: /help models or /model list [provider]",
		"  Custom models: explicit config.LoadModelsFile/LoadModelsFiles only; local models.json auto-loading is disabled",
		"  Credentials: set provider env vars or run /login <provider>.",
	}
}

func providerSummary(providers []string, counts map[string]int) string {
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		parts = append(parts, fmt.Sprintf("%s(%d)", provider, counts[provider]))
	}
	return joinStrings(parts, ", ")
}

func sortedModelsByProvider() ([]ai.Model, []string, map[string]int) {
	models := ai.ListModels()
	sort.Slice(models, func(i, j int) bool {
		leftProvider := string(models[i].Provider)
		rightProvider := string(models[j].Provider)
		if leftProvider == rightProvider {
			return models[i].ID < models[j].ID
		}
		return leftProvider < rightProvider
	})
	counts := map[string]int{}
	for _, model := range models {
		counts[string(model.Provider)]++
	}
	providers := make([]string, 0, len(counts))
	for provider := range counts {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	return models, providers, counts
}

func unknownModelError(provider, id string) string {
	models, providers, counts := sortedModelsByProvider()
	if _, ok := counts[provider]; !ok {
		return fmt.Sprintf("unknown provider '%s'. Known providers: %s", provider, providerSummary(providers, counts))
	}
	candidates := []string{}
	for _, model := range models {
		if string(model.Provider) == provider {
			candidates = append(candidates, model.ID)
			if len(candidates) == 12 {
				break
			}
		}
	}
	more := ""
	if counts[provider] > 12 {
		more = fmt.Sprintf("; run /model list %s for all %d models", provider, counts[provider])
	}
	return fmt.Sprintf("unknown model in catalog: %s:%s. Candidates: %s%s", provider, id, joinStrings(candidates, ", "), more)
}

func modelCredentialHint(provider string) string {
	return config.ModelCredentialHint(provider)
}

func ModelCredentialHint(provider string) string {
	return modelCredentialHint(provider)
}

func skillHint(name string, available []skills.Skill) string {
	matches := matchingSkillNames(name, available, func(skillName string) bool { return stringsHasPrefix(skillName, name) })
	if len(matches) == 0 {
		matches = matchingSkillNames(name, available, func(skillName string) bool { return stringsContains(skillName, name) })
	}
	if len(matches) == 0 {
		return ""
	}
	return " Did you mean: " + joinStrings(matches, ", ") + "?"
}

func matchingSkillNames(name string, available []skills.Skill, match func(string) bool) []string {
	matches := []string{}
	for _, skill := range available {
		if match(skill.Name) {
			matches = append(matches, skill.Name)
			if len(matches) == 5 {
				break
			}
		}
	}
	return matches
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func stringsContains(value, needle string) bool {
	if needle == "" {
		return true
	}
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func joinStrings(values []string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += separator + value
	}
	return out
}

func splitCommandArgs(input string) []string {
	var argv []string
	current := make([]rune, 0, len(input))
	inQuotes := false
	for _, char := range input {
		if char == '"' {
			inQuotes = !inQuotes
			continue
		}
		if char == ' ' || char == '\t' {
			if inQuotes {
				current = append(current, char)
				continue
			}
			if len(current) > 0 {
				argv = append(argv, string(current))
				current = current[:0]
			}
			continue
		}
		current = append(current, char)
	}
	if len(current) > 0 {
		argv = append(argv, string(current))
	}
	return argv
}

func trimLeftSpace(input string) string {
	for len(input) > 0 {
		char, size := utf8.DecodeRuneInString(input)
		if !unicode.IsSpace(char) {
			return input
		}
		input = input[size:]
	}
	return input
}
