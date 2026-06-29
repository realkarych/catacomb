package memory_test

import (
	"testing"

	"github.com/realkarych/catacomb/store"
	"github.com/realkarych/catacomb/store/memory"
	"github.com/realkarych/catacomb/store/storetest"
)

func TestMemoryContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) store.Store {
		return memory.New()
	})
}
