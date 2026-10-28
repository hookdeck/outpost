package apirouter

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/workloadidentity"
)

// workloadIdentityPath is where the issuer's discovery document and JWKS are
// served; the configured issuer URL is the public URL of this path.
const workloadIdentityPath = "/workload-identity"

type WorkloadIdentityHandlers struct {
	issuer *workloadidentity.Issuer
}

func NewWorkloadIdentityHandlers(issuer *workloadidentity.Issuer) *WorkloadIdentityHandlers {
	return &WorkloadIdentityHandlers{issuer: issuer}
}

func (h *WorkloadIdentityHandlers) Discovery(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.issuer.Discovery())
}

func (h *WorkloadIdentityHandlers) JWKS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.issuer.JWKS())
}

type WorkloadIdentityResponse struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// RetrieveTenant returns the values a tenant configures in its cloud
// provider to trust tokens issued for it.
func (h *WorkloadIdentityHandlers) RetrieveTenant(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	c.JSON(http.StatusOK, WorkloadIdentityResponse{
		Issuer:  h.issuer.URL(),
		Subject: workloadidentity.TenantSubject(tenant.ID),
	})
}
