package provisioner

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/smallstep/cli/jose"
)

// gcpPayload extends jwt.Claims with custom GCP attributes.
type gcpPayload struct {
	jose.Claims
	AuthorizedParty string           `json:"azp"`
	Email           string           `json:"email"`
	EmailVerified   bool             `json:"email_verified"`
	Google          gcpGooglePayload `json:"google"`
}

type gcpGooglePayload struct {
	ComputeEngine gcpComputeEnginePayload `json:"compute_engine"`
}

type gcpComputeEnginePayload struct {
	InstanceID                string            `json:"instance_id"`
	InstanceName              string            `json:"instance_name"`
	InstanceCreationTimestamp *jose.NumericDate `json:"instance_creation_timestamp"`
	ProjectID                 string            `json:"project_id"`
	ProjectNumber             int64             `json:"project_number"`
	Zone                      string            `json:"zone"`
	LicenseID                 []string          `json:"license_id"`
}

// GCP is the provisioner that supports identity tokens created by the Google
// Cloud Platform metadata API.
type GCP struct {
	Type            string   `json:"type"`
	Name            string   `json:"name"`
	ServiceAccounts []string `json:"serviceAccounts"`
	Claims          *Claims  `json:"claims,omitempty"`
	claimer         *Claimer
	certStore       *keyStore
}

// GetID returns the provisioner unique identifier. The name should uniquely
// identify any GCP provisioner.
func (p *GCP) GetID() string {
	return "gcp:" + p.Name
}

// GetTokenID returns the identifier of the token. For GCP this is the sha256 of
// "instance_id.iat.exp".
func (p *GCP) GetTokenID(token string) (string, error) {
	jwt, err := jose.ParseSigned(token)
	if err != nil {
		return "", errors.Wrap(err, "error parsing token")
	}

	// Get claims w/out verification.
	var claims gcpPayload
	if err = jwt.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return "", errors.Wrap(err, "error verifying claims")
	}

	unique := fmt.Sprintf("%s.%d.%d", claims.Google.ComputeEngine.InstanceID, claims.IssuedAt, claims.Expiry)
	sum := sha256.Sum256([]byte(unique))
	return strings.ToLower(hex.EncodeToString(sum[:])), nil
}

// GetName returns the name of the provisioner.
func (p *GCP) GetName() string {
	return p.Name
}

// GetType returns the type of provisioner.
func (p *GCP) GetType() Type {
	return TypeGCP
}

// GetEncryptedKey is not available in a GCP provisioner.
func (p *GCP) GetEncryptedKey() (kid string, key string, ok bool) {
	return "", "", false
}

// Init validates and initializes the GCP provider.
func (p *GCP) Init(config Config) error {
	var err error
	switch {
	case p.Type == "":
		return errors.New("provisioner type cannot be empty")
	case p.Name == "":
		return errors.New("provisioner name cannot be empty")
	}
	// Update claims with global ones
	if p.claimer, err = NewClaimer(p.Claims, config.Claims); err != nil {
		return err
	}
	// Initialize certificate store
	p.certStore, err = newCertificateStore("https://www.googleapis.com/oauth2/v1/certs")
	if err != nil {
		return err
	}
	return nil
}

// AuthorizeSign validates the given token and returns the sign options that
// will be used on certificate creation.
func (p *GCP) AuthorizeSign(token string) ([]SignOption, error) {
	claims, err := p.authorizeToken(token)
	if err != nil {
		return nil, err
	}

	ce := claims.Google.ComputeEngine
	dnsNames := []string{
		fmt.Sprintf("%s.c.%s.internal", ce.InstanceName, ce.ProjectID),
		fmt.Sprintf("%s.%s.c.%s.internal", ce.InstanceName, ce.Zone, ce.ProjectID),
	}

	return []SignOption{
		commonNameValidator(ce.InstanceName),
		dnsNamesValidator(dnsNames),
		profileDefaultDuration(p.claimer.DefaultTLSCertDuration()),
		newProvisionerExtensionOption(TypeGCP, p.Name, claims.AuthorizedParty),
		newValidityValidator(p.claimer.MinTLSCertDuration(), p.claimer.MaxTLSCertDuration()),
	}, nil
}

// AuthorizeRenewal returns an error if the renewal is disabled.
func (p *GCP) AuthorizeRenewal(cert *x509.Certificate) error {
	if p.claimer.IsDisableRenewal() {
		return errors.Errorf("renew is disabled for provisioner %s", p.GetID())
	}
	return nil
}

// AuthorizeRevoke returns an error if the provisioner does not have rights to
// revoke a certificate.
func (p *GCP) AuthorizeRevoke(token string) error {
	_, err := p.authorizeToken(token)
	return err
}

// GetIdentityURL returns the url that generates the GCP token.
func (p *GCP) GetIdentityURL() string {
	audience := url.QueryEscape(p.GetID())
	return fmt.Sprintf("http://metadata/computeMetadata/v1/instance/service-accounts/default/identity?audience=%s&format=full&licenses=FALSE", audience)
}

// GetIdentityToken does an HTTP request to the identity url.
func (p *GCP) GetIdentityToken() (string, error) {
	req, err := http.NewRequest("GET", p.GetIdentityURL(), http.NoBody)
	if err != nil {
		return "", errors.Wrap(err, "error creating identity request")
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "error doing identity request, are you in a GCP VM?")
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "error reading identity request response")
	}
	return string(bytes.TrimSpace(b)), nil
}

// authorizeToken performs common jwt authorization actions and returns the
// claims for case specific downstream parsing.
// e.g. a Sign request will auth/validate different fields than a Revoke request.
func (p *GCP) authorizeToken(token string) (*gcpPayload, error) {
	jwt, err := jose.ParseSigned(token)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing token")
	}
	if len(jwt.Headers) == 0 {
		return nil, errors.New("error parsing token: header is missing")
	}
	kid := jwt.Headers[0].KeyID
	cert := p.certStore.GetCertificate(kid)
	if cert == nil {
		return nil, errors.Errorf("failed to validate payload: cannot find certificate for kid %s", kid)
	}

	var claims gcpPayload
	if err = jwt.Claims(cert.PublicKey, &claims); err != nil {
		return nil, errors.Wrap(err, "error parsing claims")
	}

	// According to "rfc7519 JSON Web Token" acceptable skew should be no
	// more than a few minutes.
	if err = claims.ValidateWithLeeway(jose.Expected{
		Issuer:   "https://accounts.google.com",
		Time:     time.Now().UTC(),
		Audience: []string{p.GetID()},
	}, time.Minute); err != nil {
		return nil, errors.Wrapf(err, "invalid token")
	}

	// validate authorized party
	if len(p.ServiceAccounts) > 0 {
		var found bool
		for _, sa := range p.ServiceAccounts {
			if sa == claims.AuthorizedParty {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("invalid token: invalid authorized party claim (azp)")
		}
	}

	switch {
	case claims.Google.ComputeEngine.InstanceID == "":
		return nil, errors.New("token google.compute_engine.instance_id cannot be empty")
	case claims.Google.ComputeEngine.ProjectID == "":
		return nil, errors.New("token google.compute_engine.project_id cannot be empty")
	case claims.Google.ComputeEngine.Zone == "":
		return nil, errors.New("token google.compute_engine.zone cannot be empty")
	}

	return &claims, nil
}