package ociwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	ocicore "github.com/oracle/oci-go-sdk/v65/core"
)

// ociCallTimeout bounds one ListInstances attempt well under the tick
// interval, so a hanging API can never stall the checks.
const ociCallTimeout = 3 * time.Minute

// InstanceLister is the seam between the watcher and OCI; unit tests fake it.
type InstanceLister interface {
	List(ctx context.Context) ([]Instance, error)
}

// ociCollector lists instances with the VM's instance principal. It is
// read-only and the only OCI call the bot ever makes (anti-goals: no OCI
// writes, no keys on the VM). The tenancy root compartment is used, which
// sees every instance including future Micros (design §3).
type ociCollector struct {
	log     *slog.Logger
	ready   bool
	client  ocicore.ComputeClient
	tenancy string
}

// NewOCICollector returns the real, instance-principal-backed lister.
func NewOCICollector(log *slog.Logger) InstanceLister {
	return &ociCollector{log: log}
}

// List builds the instance-principal client lazily on the first call; if
// that fails (say the metadata endpoint is briefly unreachable at boot) the
// watcher's OCI-reachable check reports it and the next tick retries.
func (c *ociCollector) List(ctx context.Context) ([]Instance, error) {
	if !c.ready {
		provider, err := auth.InstancePrincipalConfigurationProvider()
		if err != nil {
			return nil, fmt.Errorf("instance principal: %w", err)
		}
		tenancy, err := tenancyOCID(provider)
		if err != nil {
			return nil, fmt.Errorf("tenancy OCID: %w", err)
		}
		client, err := ocicore.NewComputeClientWithConfigurationProvider(provider)
		if err != nil {
			return nil, fmt.Errorf("compute client: %w", err)
		}
		c.client, c.tenancy, c.ready = client, tenancy, true
		c.log.Info("OCI instance-principal client ready", "tenancy", tenancy)
	}

	var out []Instance
	page := ""
	for {
		req := ocicore.ListInstancesRequest{CompartmentId: &c.tenancy}
		if page != "" {
			req.Page = &page
		}
		resp, err := c.client.ListInstances(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list instances: %w", err)
		}
		for _, inst := range resp.Items {
			name := ""
			if inst.DisplayName != nil {
				name = *inst.DisplayName
			}
			out = append(out, Instance{Name: name, State: string(inst.LifecycleState)})
		}
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			return out, nil
		}
		page = *resp.OpcNextPage
	}
}

// tenancyOCID prefers the provider's own value and falls back to the
// instance metadata endpoint: instance-principal policies do not allow
// Identity calls, so the OCID must come from one of these two places
// (design §3).
func tenancyOCID(provider common.ConfigurationProvider) (string, error) {
	if tenancy, err := provider.TenancyOCID(); err == nil && tenancy != "" {
		return tenancy, nil
	}
	return metadataTenancy()
}

const metadataInstanceURL = "http://169.254.169.254/opc/v2/instance/"

func metadataTenancy() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataInstanceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer Oracle")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var meta struct {
		TenantID string `json:"tenantId"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", err
	}
	if meta.TenantID == "" {
		return "", errors.New("metadata endpoint returned no tenantId")
	}
	return meta.TenantID, nil
}
