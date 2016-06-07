package libkbfs

import (
	"errors"
	"sort"

	"github.com/keybase/client/go/protocol"
)

// BareTlfHandle uniquely identifies top-level folders by readers and
// writers.
//
// TODO: Have separate types for writers vs. readers.
type BareTlfHandle struct {
	Writers           []keybase1.UID             `codec:"w,omitempty"`
	Readers           []keybase1.UID             `codec:"r,omitempty"`
	UnresolvedWriters []keybase1.SocialAssertion `codec:"uw,omitempty"`
	UnresolvedReaders []keybase1.SocialAssertion `codec:"ur,omitempty"`
	ConflictInfo      *ConflictInfo              `codec:"ci,omitempty"`
}

// ErrNoWriters is the error returned by MakeBareTlfHandle if it is
// passed an empty list of writers.
var ErrNoWriters = errors.New("Cannot make TLF handle with no writers; need rekey?")

// ErrInvalidWriter is the error returned by MakeBareTlfHandle if it
// is passed an invalid writer.
var ErrInvalidWriter = errors.New("Cannot make TLF handle with invalid writer")

// ErrInvalidReader is the error returned by MakeBareTlfHandle if it
// is passed an invalid reader.
var ErrInvalidReader = errors.New("Cannot make TLF handle with invalid reader")

// UIDList can be used to lexicographically sort UIDs.
type UIDList []keybase1.UID

func (u UIDList) Len() int {
	return len(u)
}

func (u UIDList) Less(i, j int) bool {
	return u[i].Less(u[j])
}

func (u UIDList) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

// SocialAssertionList can be used to lexicographically sort SocialAssertions.
type SocialAssertionList []keybase1.SocialAssertion

func (u SocialAssertionList) Len() int {
	return len(u)
}

func (u SocialAssertionList) Less(i, j int) bool {
	si := u[i].String()
	sj := u[j].String()
	return si < sj
}

func (u SocialAssertionList) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

// MakeBareTlfHandle creates a BareTlfHandle from the given list of
// readers and writers.
func MakeBareTlfHandle(
	writers, readers []keybase1.UID,
	unresolvedWriters, unresolvedReaders []keybase1.SocialAssertion,
	conflictInfo *ConflictInfo) (BareTlfHandle, error) {
	if len(writers) == 0 {
		return BareTlfHandle{}, ErrNoWriters
	}

	for _, w := range writers {
		if w == keybase1.PUBLIC_UID {
			return BareTlfHandle{}, ErrInvalidWriter
		}
	}

	if (len(readers) + len(unresolvedReaders)) > 1 {
		// If we have more than one reader, none of them
		// should be the public UID.
		for _, r := range readers {
			if r == keybase1.PUBLIC_UID {
				return BareTlfHandle{}, ErrInvalidReader
			}
		}
	}

	// TODO: Check for overlap between readers and writers, and
	// for duplicates.

	writersCopy := make([]keybase1.UID, len(writers))
	copy(writersCopy, writers)
	sort.Sort(UIDList(writersCopy))

	var readersCopy []keybase1.UID
	if len(readers) > 0 {
		readersCopy = make([]keybase1.UID, len(readers))
		copy(readersCopy, readers)
		sort.Sort(UIDList(readersCopy))
	}

	var unresolvedWritersCopy []keybase1.SocialAssertion
	if len(unresolvedWriters) > 0 {
		unresolvedWritersCopy = make([]keybase1.SocialAssertion, len(unresolvedWriters))
		copy(unresolvedWritersCopy, unresolvedWriters)
		sort.Sort(SocialAssertionList(unresolvedWritersCopy))
	}

	var unresolvedReadersCopy []keybase1.SocialAssertion
	if len(unresolvedReaders) > 0 {
		unresolvedReadersCopy = make([]keybase1.SocialAssertion, len(unresolvedReaders))
		copy(unresolvedReadersCopy, unresolvedReaders)
		sort.Sort(SocialAssertionList(unresolvedReadersCopy))
	}

	return BareTlfHandle{
		Writers:           writersCopy,
		Readers:           readersCopy,
		UnresolvedWriters: unresolvedWritersCopy,
		UnresolvedReaders: unresolvedReadersCopy,
		ConflictInfo:      conflictInfo,
	}, nil
}

func resolveAssertions(assertions map[keybase1.SocialAssertion]keybase1.UID,
	unresolved []keybase1.SocialAssertion, resolved []keybase1.UID) (
	map[keybase1.UID]bool, []keybase1.SocialAssertion) {
	resolvedMap := uidSliceToSet(resolved)
	unresolvedMap := assertionSliceToSet(unresolved)
	for a, u := range assertions {
		if unresolvedMap[a] {
			resolvedMap[u] = true
			delete(unresolvedMap, a)
		}
	}
	return resolvedMap, assertionSetToSlice(unresolvedMap)
}

func uidSetToSlice(m map[keybase1.UID]bool) (s []keybase1.UID) {
	for u := range m {
		s = append(s, u)
	}
	return s
}

func assertionSetToSlice(m map[keybase1.SocialAssertion]bool) (s []keybase1.SocialAssertion) {
	for u := range m {
		s = append(s, u)
	}
	return s
}

func uidSliceToSet(s []keybase1.UID) map[keybase1.UID]bool {
	m := make(map[keybase1.UID]bool, len(s))
	for _, u := range s {
		m[u] = true
	}
	return m
}

func assertionSliceToSet(s []keybase1.SocialAssertion) map[keybase1.SocialAssertion]bool {
	m := make(map[keybase1.SocialAssertion]bool, len(s))
	for _, u := range s {
		m[u] = true
	}
	return m
}

// ResolveAssertions creates a new BareTlfHandle given an existing one with
// while resolving the passed assertions.
func (h BareTlfHandle) ResolveAssertions(
	assertions map[keybase1.SocialAssertion]keybase1.UID) BareTlfHandle {
	if len(assertions) == 0 || (len(h.UnresolvedWriters) == 0 && len(h.UnresolvedReaders) == 0) {
		return h
	}
	var resolvedWriters, resolvedReaders map[keybase1.UID]bool
	resolvedWriters, h.UnresolvedWriters = resolveAssertions(assertions, h.UnresolvedWriters, h.Writers)
	resolvedReaders, h.UnresolvedReaders = resolveAssertions(assertions, h.UnresolvedReaders, h.Readers)
	h.Writers = uidSetToSlice(resolvedWriters)
	for _, u := range h.Writers {
		delete(resolvedReaders, u)
	}
	h.Readers = uidSetToSlice(resolvedReaders)
	sort.Sort(UIDList(h.Writers))
	sort.Sort(UIDList(h.Readers))
	sort.Sort(SocialAssertionList(h.UnresolvedWriters))
	sort.Sort(SocialAssertionList(h.UnresolvedReaders))
	return h
}

// IsPublic returns whether or not this BareTlfHandle represents a
// public top-level folder.
func (h BareTlfHandle) IsPublic() bool {
	return len(h.Readers) == 1 && h.Readers[0].Equal(keybase1.PublicUID)
}

func (h BareTlfHandle) findUserInList(user keybase1.UID,
	users []keybase1.UID) bool {
	// TODO: this could be more efficient with a cached map/set
	for _, u := range users {
		if u == user {
			return true
		}
	}
	return false
}

// IsWriter returns whether or not the given user is a writer for the
// top-level folder represented by this BareTlfHandle.
func (h BareTlfHandle) IsWriter(user keybase1.UID) bool {
	return h.findUserInList(user, h.Writers)
}

// IsReader returns whether or not the given user is a reader for the
// top-level folder represented by this BareTlfHandle.
func (h BareTlfHandle) IsReader(user keybase1.UID) bool {
	return h.IsPublic() || h.findUserInList(user, h.Readers) || h.IsWriter(user)
}

// ResolvedUsers returns a list of all resolved writer and reader
// UIDs. Note that if the handle is public, the returned list won't
// contain PUBLIC_UID.
func (h BareTlfHandle) ResolvedUsers() []keybase1.UID {
	var resolvedUsers []keybase1.UID
	resolvedUsers = append(resolvedUsers, h.Writers...)
	if !h.IsPublic() {
		resolvedUsers = append(resolvedUsers, h.Readers...)
	}
	return resolvedUsers
}

// UnresolvedUsers returns a list of all unresolved writers and
// readers.
func (h BareTlfHandle) UnresolvedUsers() []keybase1.SocialAssertion {
	var unresolvedUsers []keybase1.SocialAssertion
	unresolvedUsers = append(unresolvedUsers, h.UnresolvedWriters...)
	unresolvedUsers = append(unresolvedUsers, h.UnresolvedReaders...)
	return unresolvedUsers
}
