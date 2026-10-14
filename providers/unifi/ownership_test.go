package unifi

import (
	"testing"

	"github.com/maxfield-allison/dnsweaver/internal/testutil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestUniFiOwnershipMigrationConformance(t *testing.T) {
	server := newFakeController(t).server()
	defer server.Close()
	testutil.RunOwnershipMigrationConformance(t, func(t *testing.T, owner, storage string) provider.Provider {
		return testutil.OwnershipRegistryProvider(t, owner, storage, "unifi", Factory(),
			map[string]string{"URL": server.URL, "API_KEY": testAPIKey, "SITE": "default", "ZONE": "example.com"})
	})
}
