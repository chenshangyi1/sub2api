package operations

import (
	"testing"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
)

func TestListAllTargetAccountsLoadsEveryPage(t *testing.T) {
	calls := 0
	items, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		calls++
		if pageSize != 1000 {
			t.Fatalf("page size = %d", pageSize)
		}
		if page == 1 {
			batch := make([]sub2api.AdminAccount, pageSize)
			for i := range batch {
				batch[i].ID = int64(i + 1)
			}
			return batch, nil
		}
		return []sub2api.AdminAccount{{ID: 1001}}, nil
	})
	if err != nil {
		t.Fatalf("list all accounts: %v", err)
	}
	if calls != 2 || len(items) != 1001 || items[1000].ID != 1001 {
		t.Fatalf("calls=%d len=%d last=%d", calls, len(items), items[len(items)-1].ID)
	}
}

func TestListAllTargetAccountsRejectsRepeatedFullPage(t *testing.T) {
	batch := make([]sub2api.AdminAccount, 1000)
	for i := range batch {
		batch[i].ID = int64(i + 1)
	}
	_, err := listAllTargetAccounts(func(int, int) ([]sub2api.AdminAccount, error) {
		return batch, nil
	})
	if err == nil {
		t.Fatal("expected pagination progress error")
	}
}
