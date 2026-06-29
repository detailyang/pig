package session

import (
	"strings"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type Session struct {
	storage Storage
}

func NewSession(storage Storage) *Session { return &Session{storage: storage} }

func (session *Session) Storage() Storage { return session.storage }

func (session *Session) LeafID() (*string, error) { return session.storage.GetLeafID() }

func (session *Session) LeafId() (*string, error) { return session.LeafID() }

func (session *Session) GetEntry(id string) (*Entry, error) { return session.storage.GetEntry(id) }

func (session *Session) Entries() ([]Entry, error) { return session.storage.GetEntries() }

func (session *Session) Branch(fromID *string) ([]Entry, error) {
	leaf := fromID
	if leaf == nil {
		var err error
		leaf, err = session.storage.GetLeafID()
		if err != nil {
			return nil, err
		}
	}
	return session.storage.GetPathToRoot(leaf)
}

func (session *Session) BuildContext() (Context, error) {
	branch, err := session.Branch(nil)
	if err != nil {
		return Context{}, err
	}
	return BuildSessionContext(branch), nil
}

func (session *Session) Label(id string) (*string, error) { return session.storage.GetLabel(id) }

func (session *Session) SessionName() (*string, error) {
	entries, err := session.storage.FindEntries(EntryTypeSessionInfo)
	if err != nil {
		return nil, err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].Name != nil {
			name := strings.TrimSpace(*entries[index].Name)
			if name != "" {
				return &name, nil
			}
		}
	}
	return nil, nil
}

func (session *Session) appendTyped(entry Entry) (string, error) {
	if err := session.storage.AppendEntry(entry); err != nil {
		return "", err
	}
	return entry.ID(), nil
}

func (session *Session) AppendMessage(message agent.Message) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(NewMessageEntry(id, parent, CreateTimestamp(), message))
}

func (session *Session) AppendThinkingLevelChange(thinkingLevel string) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(Entry{EntryType: EntryTypeThinkingLevelChange, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), ThinkingLevel: thinkingLevel})
}

func (session *Session) AppendModelChange(provider, modelID string) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(Entry{EntryType: EntryTypeModelChange, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), Provider: provider, ModelID: modelID})
}

func (session *Session) AppendCompaction(summary, firstKeptEntryID string, tokensBefore uint64, details any, fromHook bool) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(NewCompactionEntry(id, parent, CreateTimestamp(), summary, firstKeptEntryID, tokensBefore, details, fromHook))
}

func (session *Session) AppendCustom(customType string, data any) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(Entry{EntryType: EntryTypeCustom, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), CustomType: customType, Data: data})
}

func (session *Session) AppendSystemPrompt(message ai.Message) (string, error) {
	return session.AppendCustom("system_prompt", map[string]any{"message": message})
}

func (session *Session) AppendCustomMessage(customType string, content any, details any, display bool) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	entry := Entry{EntryType: EntryTypeCustomMessage, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), CustomType: customType, Content: content, DetailsValue: details, Display: display}
	if details, ok := details.(map[string]any); ok {
		entry.Details = details
	}
	return session.appendTyped(entry)
}

func (session *Session) AppendLabel(targetID string, label *string) (string, error) {
	entry, err := session.storage.GetEntry(targetID)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", Error{Code: ErrorNotFound, Message: "Entry " + targetID + " not found"}
	}
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	return session.appendTyped(Entry{EntryType: EntryTypeLabel, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), TargetID: &targetID, LabelValue: label})
}

func (session *Session) AppendSessionName(name string) (string, error) {
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return "", err
	}
	parent, err := session.storage.GetLeafID()
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(name)
	return session.appendTyped(Entry{EntryType: EntryTypeSessionInfo, EntryID: id, ParentID: parent, Timestamp: CreateTimestamp(), Name: &trimmed})
}

func (session *Session) MoveTo(entryID string, summary *BranchSummaryInput) (*string, error) {
	var targetID *string
	if entryID != "" {
		targetID = &entryID
		entry, err := session.storage.GetEntry(entryID)
		if err != nil {
			return nil, err
		}
		if entry == nil {
			return nil, Error{Code: ErrorNotFound, Message: "Entry " + entryID + " not found"}
		}
	}
	if err := session.storage.SetLeafID(targetID); err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}
	id, err := session.storage.CreateEntryID()
	if err != nil {
		return nil, err
	}
	fromID := "root"
	if targetID != nil {
		fromID = *targetID
	}
	entry := Entry{EntryType: EntryTypeBranchSummary, EntryID: id, ParentID: targetID, Timestamp: CreateTimestamp(), FromID: fromID, Summary: summary.Summary, DetailsValue: summary.Details}
	if details, ok := summary.Details.(map[string]any); ok {
		entry.Details = details
	}
	if summary.FromHook {
		entry.FromHook = boolPtr(true)
	}
	appended, err := session.appendTyped(entry)
	if err != nil {
		return nil, err
	}
	return &appended, nil
}
