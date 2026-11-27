package organization

import (
	"bytes"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/crewjam/saml"
	"github.com/pocketbase/pocketbase/core"
)

const (
	samlProtocolNamespace  = "urn:oasis:names:tc:SAML:2.0:protocol"
	samlAssertionNamespace = "urn:oasis:names:tc:SAML:2.0:assertion"
)

type samlProtocolError struct {
	code string
	err  error
}

func (failure *samlProtocolError) Error() string {
	if failure == nil || failure.err == nil {
		return leafen.ErrorCodeSAMLResponseInvalid
	}
	return failure.err.Error()
}

func (failure *samlProtocolError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.err
}

func newSAMLProtocolError(code string, message string) error {
	return &samlProtocolError{code: code, err: errors.New(message)}
}

func samlProtocolFailureCode(err error) string {
	failure := &samlProtocolError{}
	if errors.As(err, &failure) && failure.code != "" {
		return failure.code
	}
	return leafen.ErrorCodeSAMLResponseInvalid
}

func validateSAMLRelayState(value string) (string, error) {
	if value == "" {
		return "/", nil
	}
	if len(value) > leafen.SAMLMaxRelayStateBytes || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\x00\r\n") {
		return "", newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "invalid SAML RelayState")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "invalid SAML RelayState")
	}
	return value, nil
}

func validateSAMLRequestCorrelation(inResponseTo string, possibleRequestIDs []string, idpInitiated bool) error {
	if idpInitiated {
		if inResponseTo == "" && len(possibleRequestIDs) == 0 {
			return nil
		}
		return newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "unsolicited SAML response contains request correlation")
	}
	if inResponseTo == "" || len(possibleRequestIDs) != 1 || inResponseTo != possibleRequestIDs[0] {
		return newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "SAML request correlation does not match")
	}
	return nil
}

func preflightSAMLResponse(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML response is empty")
	}
	if len(encoded) > leafen.SAMLMaxEncodedResponseBytes {
		return nil, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseTooLarge, "encoded SAML response is too large")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML response is not valid base64")
	}
	if len(decoded) > leafen.SAMLMaxXMLResponseBytes {
		return nil, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseTooLarge, "SAML XML response is too large")
	}
	if err := validateSAMLXMLShape(decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func validateSAMLXMLShape(decoded []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(decoded))
	decoder.Strict = true
	tokens := 0
	depth := 0
	assertions := 0
	attributes := 0
	attributeDepth := 0
	attributeBytes := 0
	rootValid := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML XML is malformed")
		}
		tokens++
		if tokens > leafen.SAMLMaxXMLTokens {
			return newSAMLProtocolError(leafen.ErrorCodeSAMLXMLTooComplex, "SAML XML is too complex")
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				rootValid = value.Name.Space == samlProtocolNamespace && value.Name.Local == "Response"
			}
			if value.Name.Space == samlAssertionNamespace && value.Name.Local == "Assertion" {
				assertions++
			}
			if value.Name.Space == samlAssertionNamespace && value.Name.Local == "Attribute" {
				attributes++
				attributeDepth = depth
				if attributes > leafen.SAMLMaxAttributes {
					return newSAMLProtocolError(leafen.ErrorCodeSAMLAttributesTooLarge, "SAML attributes exceed the count bound")
				}
			}
			if value.Name.Local == "SignatureMethod" || value.Name.Local == "DigestMethod" {
				for _, attribute := range value.Attr {
					if attribute.Name.Local == "Algorithm" && !strongSAMLAlgorithm(attribute.Value) {
						return newSAMLProtocolError(leafen.ErrorCodeSAMLWeakAlgorithm, "SAML signature algorithm is not SHA-256 or stronger")
					}
				}
			}
		case xml.EndElement:
			if attributeDepth == depth && value.Name.Space == samlAssertionNamespace && value.Name.Local == "Attribute" {
				attributeDepth = 0
			}
			depth--
		case xml.CharData:
			if attributeDepth > 0 {
				attributeBytes += len(value)
				if attributeBytes > leafen.SAMLMaxAttributeTotalBytes {
					return newSAMLProtocolError(leafen.ErrorCodeSAMLAttributesTooLarge, "SAML attributes exceed the byte bound")
				}
			}
		case xml.Directive:
			return newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML XML directives are not allowed")
		case xml.ProcInst:
			if !strings.EqualFold(value.Target, "xml") {
				return newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML XML processing instructions are not allowed")
			}
		}
	}
	if !rootValid {
		return newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML XML root is invalid")
	}
	if assertions != 1 {
		return newSAMLProtocolError(leafen.ErrorCodeSAMLAssertionCardinalityInvalid, "SAML response must contain exactly one assertion")
	}
	return nil
}

func strongSAMLAlgorithm(algorithm string) bool {
	algorithm = strings.ToLower(strings.TrimSpace(algorithm))
	if strings.Contains(algorithm, "sha1") {
		return false
	}
	return strings.Contains(algorithm, "sha256") || strings.Contains(algorithm, "sha384") ||
		strings.Contains(algorithm, "sha512")
}

type samlEnvelope struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol Response"`
	InResponseTo string   `xml:"InResponseTo,attr"`
	Issuer       struct {
		Value string `xml:",chardata"`
	} `xml:"urn:oasis:names:tc:SAML:2.0:assertion Issuer"`
	Assertion struct {
		ID     string `xml:"ID,attr"`
		Issuer struct {
			Value string `xml:",chardata"`
		} `xml:"urn:oasis:names:tc:SAML:2.0:assertion Issuer"`
		SubjectConfirmations []struct {
			Data struct {
				InResponseTo string `xml:"InResponseTo,attr"`
			} `xml:"urn:oasis:names:tc:SAML:2.0:assertion SubjectConfirmationData"`
		} `xml:"urn:oasis:names:tc:SAML:2.0:assertion Subject>SubjectConfirmation"`
	} `xml:"urn:oasis:names:tc:SAML:2.0:assertion Assertion"`
}

func readSAMLEnvelope(decoded []byte) (samlEnvelope, error) {
	envelope := samlEnvelope{}
	if err := xml.Unmarshal(decoded, &envelope); err != nil {
		return envelope, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML response envelope is invalid")
	}
	if envelope.Issuer.Value == "" {
		envelope.Issuer.Value = envelope.Assertion.Issuer.Value
	}
	if strings.TrimSpace(envelope.Issuer.Value) == "" || strings.TrimSpace(envelope.Assertion.ID) == "" {
		return envelope, newSAMLProtocolError(leafen.ErrorCodeSAMLResponseInvalid, "SAML response identity is missing")
	}
	return envelope, nil
}

func validateSAMLEnvelopeCorrelation(envelope samlEnvelope, requestIDs []string, idpInitiated bool) error {
	if err := validateSAMLRequestCorrelation(envelope.InResponseTo, requestIDs, idpInitiated); err != nil {
		return err
	}
	for _, confirmation := range envelope.Assertion.SubjectConfirmations {
		if err := validateSAMLRequestCorrelation(confirmation.Data.InResponseTo, requestIDs, idpInitiated); err != nil {
			return err
		}
	}
	return nil
}

func samlAssertionEmail(assertion *saml.Assertion, emailAttribute string) (string, error) {
	if assertion == nil || assertion.Subject == nil {
		return "", newSAMLProtocolError(leafen.ErrorCodeSAMLIdentityDenied, "SAML identity has no subject")
	}
	emailAttribute = strings.TrimSpace(emailAttribute)
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name != emailAttribute && attribute.FriendlyName != emailAttribute {
				continue
			}
			if len(attribute.Values) != 1 {
				return "", newSAMLProtocolError(leafen.ErrorCodeSAMLIdentityDenied, "SAML email attribute is ambiguous")
			}
			return normalizeSAMLIdentityEmail(attribute.Values[0].Value)
		}
	}
	if assertion.Subject.NameID == nil {
		return "", newSAMLProtocolError(leafen.ErrorCodeSAMLIdentityDenied, "SAML identity has no email")
	}
	return normalizeSAMLIdentityEmail(assertion.Subject.NameID.Value)
}

func normalizeSAMLIdentityEmail(value string) (string, error) {
	email, ok := leafen.NormalizeEmailIdentity(value)
	if !ok {
		return "", newSAMLProtocolError(leafen.ErrorCodeSAMLIdentityDenied, "SAML identity email is invalid")
	}
	return email, nil
}

func wrapSAMLProviderError(err error) error {
	if err == nil {
		return nil
	}
	return &samlProtocolError{code: leafen.ErrorCodeSAMLResponseInvalid, err: fmt.Errorf("SAML provider validation failed: %w", err)}
}

// newSAMLServiceProvider is the service provider a sign-in starts and is
// answered with, at the values the identity provider has (kept); without
// kept, at the address the request came to, for the metadata read there.
func newSAMLServiceProvider(app core.App, request *http.Request, config samlProviderConfig, kept bool) (*saml.ServiceProvider, error) {
	certificateBlock, certificateRest := pem.Decode([]byte(config.Certificate))
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" ||
		len(bytes.TrimSpace(certificateRest)) != 0 {
		return nil, leafen.ErrSAMLConfigurationInvalid
	}
	certificate := base64.StdEncoding.EncodeToString(certificateBlock.Bytes)
	origin, err := leafen.PublicServerOrigin(request)
	if err != nil {
		return nil, err
	}
	// After an address change, the identity provider's own (enterprise_saml_address.go).
	if kept {
		origin = samlServiceOrigin(app, config, origin)
	} else if earlier := leafen.EarlierAddressOf(request); earlier != "" {
		origin = earlier
	}
	metadataURL, err := url.Parse(origin + leafen.APIRoot + "/auth/saml/" + config.ID + "/metadata")
	if err != nil {
		return nil, err
	}
	acsURL, err := url.Parse(origin + leafen.APIRoot + "/auth/saml/acs")
	if err != nil {
		return nil, err
	}
	return &saml.ServiceProvider{
		EntityID:    metadataURL.String(),
		MetadataURL: *metadataURL,
		AcsURL:      *acsURL,
		IDPMetadata: &saml.EntityDescriptor{
			EntityID: config.Issuer,
			IDPSSODescriptors: []saml.IDPSSODescriptor{{
				SingleSignOnServices: []saml.Endpoint{{
					Binding: saml.HTTPRedirectBinding, Location: config.LoginURL,
				}},
			}},
		},
		IDPCertificate:     &certificate,
		AuthnNameIDFormat:  saml.EmailAddressNameIDFormat,
		AllowIDPInitiated:  config.AllowIDPInitiated != 0,
		DefaultRedirectURI: "/",
	}, nil
}

func parseVerifiedSAMLResponse(
	provider *saml.ServiceProvider,
	decoded []byte,
	requestIDs []string,
) (*saml.Assertion, error) {
	assertion, err := provider.ParseXMLResponse(decoded, requestIDs, provider.AcsURL)
	if err != nil {
		return nil, wrapSAMLProviderError(err)
	}
	return assertion, nil
}

// SAML authentication contexts that name multi-factor authentication: the
// REFEDS MFA profile and Microsoft Entra ID's multipleauthn, which Entra also
// lists in its authentication methods attribute. Classes that may be one
// factor (a smartcard, a time-synchronized token) don't count.
var samlMultiFactorContexts = map[string]bool{
	"https://refeds.org/profile/mfa":                    true,
	"http://schemas.microsoft.com/claims/multipleauthn": true,
}

const samlAuthenticationMethodsAttribute = "http://schemas.microsoft.com/claims/authnmethodsreferences"

// samlProviderSecondFactorFlag is 1 for a verified assertion whose
// authentication context names multi-factor authentication.
func samlProviderSecondFactorFlag(assertion *saml.Assertion) int {
	if assertion == nil {
		return 0
	}
	for _, statement := range assertion.AuthnStatements {
		if ref := statement.AuthnContext.AuthnContextClassRef; ref != nil && samlMultiFactorContexts[strings.TrimSpace(ref.Value)] {
			return 1
		}
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name != samlAuthenticationMethodsAttribute {
				continue
			}
			for _, value := range attribute.Values {
				if samlMultiFactorContexts[strings.TrimSpace(value.Value)] {
					return 1
				}
			}
		}
	}
	return 0
}
