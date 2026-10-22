package runs

import (
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// WithAttachments gives the run its accepted prompt attachments (ADR-0038).
// The set is persisted on the run row, reported in RunStatus.Attachments and
// handed to the runner through its ctx; st is the store that holds the set's
// files. The run's Prompt stays the user's bare text.
func WithAttachments(set attach.Set, st *attach.Store) StartOption {
	return func(o *startOptions) { o.attachments, o.attachStore = set, st }
}

// SetAttachmentStore tells the registry which attachment store a queued
// prompt's set lives in, so delivery can restore it. nil = feature off.
func (reg *Registry) SetAttachmentStore(st *attach.Store) {
	reg.mu.Lock()
	reg.attachStore = st
	reg.mu.Unlock()
}

func (reg *Registry) attachmentStore() *attach.Store {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.attachStore
}

// attachmentStartOption restores a queued item's set at delivery time. A set
// that no longer decodes, or a registry without a store, delivers the prompt
// without attachments rather than failing it.
func (reg *Registry) attachmentStartOption(raw string) []StartOption {
	st := reg.attachmentStore()
	if st == nil || raw == "" {
		return nil
	}
	set, err := attach.DecodeSet(raw)
	if err != nil || len(set.Items) == 0 {
		return nil
	}
	return []StartOption{WithAttachments(set, st)}
}

// promptAttachments is the run's attachments in the runner's neutral form;
// ok is false when the run has none.
func (r *run) promptAttachments() (taskrunner.PromptAttachments, bool) {
	if r.attachStore == nil || len(r.attachments.Items) == 0 {
		return taskrunner.PromptAttachments{}, false
	}
	st, set, text := r.attachStore, r.attachments, r.prompt
	return taskrunner.PromptAttachments{
		Render: func(native bool) ([]taskrunner.PromptImage, []string) {
			rd := st.Render(set, text, native)
			imgs := make([]taskrunner.PromptImage, len(rd.Images))
			for i, im := range rd.Images {
				imgs[i] = taskrunner.PromptImage{Name: im.Name, MimeType: im.MimeType, Path: im.Path, Size: im.Size}
			}
			return imgs, rd.Context
		},
		AddDirs: []string{st.Dir(set.ID)},
	}, true
}

// ReuseAttachment resolves a "reuse" request: the stored image/file item id of
// runID's set and the absolute path of its bytes. The source run must belong
// to taskID. It reads the run row, not the in-memory registry, so a run that
// was evicted from memory can still be reused.
func (reg *Registry) ReuseAttachment(st *attach.Store, taskID int64, runID, id string) (attach.Item, string, error) {
	if st == nil {
		return attach.Item{}, "", errors.New("attachments are not enabled")
	}
	row, err := reg.st.GetRun(runID)
	if err != nil || row.TaskID != taskID {
		return attach.Item{}, "", errors.New("source run not found")
	}
	set, it, ok := storedItem(row.Attachments, id)
	if !ok || (it.Kind != attach.KindImage && it.Kind != attach.KindFile) {
		return attach.Item{}, "", fmt.Errorf("attachment %q not found", id)
	}
	p, err := st.Path(set.ID, it)
	if err != nil {
		return attach.Item{}, "", err
	}
	return it, p, nil
}

// StoredAttachment returns runID's stored item id and its set, read from the
// run row (so it also works for a run evicted from memory).
func (reg *Registry) StoredAttachment(runID, id string) (attach.Set, attach.Item, bool) {
	row, err := reg.st.GetRun(runID)
	if err != nil {
		return attach.Set{}, attach.Item{}, false
	}
	return storedItem(row.Attachments, id)
}

func storedItem(raw, id string) (attach.Set, attach.Item, bool) {
	set, err := attach.DecodeSet(raw)
	if err != nil {
		return attach.Set{}, attach.Item{}, false
	}
	for _, it := range set.Items {
		if it.ID == id {
			return set, it, true
		}
	}
	return attach.Set{}, attach.Item{}, false
}

func decodeStoredAttachments(raw string) attach.Set {
	set, err := attach.DecodeSet(raw)
	if err != nil {
		return attach.Set{}
	}
	return set
}

func publicItems(set attach.Set) []attach.Item {
	if len(set.Items) == 0 {
		return nil
	}
	return set.Public()
}

// AttachmentStore returns the attachment store, nil when the feature is off.
func (reg *Registry) AttachmentStore() *attach.Store { return reg.attachmentStore() }

// DiscardAttachments removes a freshly accepted set whose start failed. A
// set that a queue item or run row did reference (a failure after the
// enqueue, e.g. the interrupt's stop) is left alone.
func (reg *Registry) DiscardAttachments(set attach.Set) {
	st := reg.attachmentStore()
	if st == nil || set.ID == "" {
		return
	}
	if ref, err := reg.st.ReferencedAttachmentSets(); err != nil || ref[set.ID] {
		return
	}
	_ = st.Remove(set.ID)
}
