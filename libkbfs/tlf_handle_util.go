package libkbfs

import (
	"fmt"

	"github.com/keybase/client/go/protocol"
)

// bareTlfHandleMap is like BareTlfHandle but with all lists turned
// into maps.
type bareTlfHandleMap struct {
	writers           map[keybase1.UID]bool
	readers           map[keybase1.UID]bool
	unresolvedWriters map[keybase1.SocialAssertion]bool
	unresolvedReaders map[keybase1.SocialAssertion]bool
	conflictInfo      *TlfHandleExtension
	finalizedInfo     *TlfHandleExtension
}

func checkTlfHandleExtension(
	codec Codec, name string, old, new *TlfHandleExtension) error {
	if new == nil {
		if old != nil {
			return fmt.Errorf("%s cannot disappear", name)
		}
		return nil
	}

	if old == nil {
		return nil
	}

	eq, err := CodecEqual(codec, old, new)
	if err != nil {
		return err
	}
	if !eq {
		return fmt.Errorf("%s must be the same", name)
	}

	return nil
}

func (h bareTlfHandleMap) assertValid() {
	for w := range h.writers {
		if h.readers[w] {
			panic("writer in readers")
		}
	}

	for r := range h.readers {
		if h.writers[r] {
			panic("reader in writers")
		}
	}

	for uw := range h.unresolvedWriters {
		if h.unresolvedReaders[uw] {
			panic("unresolved writer in unresolved readers")
		}
	}

	for ur := range h.unresolvedReaders {
		if h.unresolvedWriters[ur] {
			panic("unresolved reader in unresolved writers")
		}
	}
}

func (h bareTlfHandleMap) checkCanResolveTo(
	codec Codec, other bareTlfHandleMap) error {
	h.assertValid()
	other.assertValid()

	err := checkTlfHandleExtension(
		codec, "ConflictInfo", h.conflictInfo, other.conflictInfo)
	if err != nil {
		return err
	}

	err = checkTlfHandleExtension(
		codec, "FinalizedInfo", h.finalizedInfo, other.finalizedInfo)
	if err != nil {
		return err
	}

	for w := range h.writers {
		if !other.writers[w] {
			return fmt.Errorf("writer %s in old and not in new", w)
		}
	}

	for r := range h.readers {
		if !other.readers[r] && !other.writers[r] {
			return fmt.Errorf("reader %s in old and not in new", r)
		}
	}

	for uw := range other.unresolvedWriters {
		if !h.unresolvedWriters[uw] {
			return fmt.Errorf(
				"unresolved writer %s in new and not in old", uw)
		}
	}

	for ur := range other.unresolvedReaders {
		if !h.unresolvedWriters[ur] {
			return fmt.Errorf(
				"unresolved reader %s in new and not in old", ur)
		}
	}

	uuwCount := len(other.unresolvedWriters) - len(h.unresolvedWriters)
	uurCount := len(other.unresolvedReaders) - len(h.unresolvedReaders)

	for r := range other.readers {
		if h.readers[r] {
			continue
		}
		uurCount--
		if uurCount < 0 {
			return fmt.Errorf("not enough old unresolved readers to account for new readers")
		}
	}

	uwCount := 0
	for w := range other.writers {
		if h.writers[w] {
			continue
		}
		if h.readers[w] {
			uuwCount--
			if uuwCount < 0 {
				return fmt.Errorf("not enough old unresolved readers to account for promoted readers")
			}
		} else {
			uwCount++
			if uwCount > uuwCount+uurCount {
				return fmt.Errorf("not enough old unresolved users to account for new writers")
			}
		}
	}

	return nil
}
