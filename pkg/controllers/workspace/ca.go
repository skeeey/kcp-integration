package workspace

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"math/big"
	"sort"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
)

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// CertificateAuthority implements a certificate authority that supports policy
// based signing. It's used by the signing controller.
type CertificateAuthority struct {
	// RawCert is an optional field to determine if signing cert/key pairs have changed
	RawCert []byte
	// RawKey is an optional field to determine if signing cert/key pairs have changed
	RawKey []byte

	Certificate *x509.Certificate
	PrivateKey  crypto.Signer
}

// Sign signs a certificate request, applying a SigningPolicy and returns a DER
// encoded x509 certificate.
func (ca *CertificateAuthority) Sign(crDER []byte, policy SigningPolicy) ([]byte, error) {
	cr, err := x509.ParseCertificateRequest(crDER)
	if err != nil {
		return nil, fmt.Errorf("unable to parse certificate request: %v", err)
	}
	if err := cr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("unable to verify certificate request signature: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a serial number for %s: %v", cr.Subject.CommonName, err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:       serialNumber,
		Subject:            cr.Subject,
		DNSNames:           cr.DNSNames,
		IPAddresses:        cr.IPAddresses,
		EmailAddresses:     cr.EmailAddresses,
		URIs:               cr.URIs,
		PublicKeyAlgorithm: cr.PublicKeyAlgorithm,
		PublicKey:          cr.PublicKey,
		Extensions:         cr.Extensions,
		ExtraExtensions:    cr.ExtraExtensions,
	}
	if err := policy.apply(tmpl, ca.Certificate.NotAfter); err != nil {
		return nil, err
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Certificate, cr.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate: %v", err)
	}
	return der, nil
}

// SigningPolicy validates a CertificateRequest before it's signed by the
// CertificateAuthority. It may default or otherwise mutate a certificate
// template.
type SigningPolicy interface {
	// not-exporting apply forces signing policy implementations to be internal
	// to this package.
	apply(template *x509.Certificate, signerNotAfter time.Time) error
}

// PermissiveSigningPolicy is the signing policy historically used by the local
// signer.
//
//   - It forwards all SANs from the original signing request.
//   - It sets allowed usages as configured in the policy.
//   - It zeros all extensions.
//   - It sets BasicConstraints to true.
//   - It sets IsCA to false.
//   - It validates that the signer has not expired.
//   - It sets NotBefore and NotAfter:
//     All certificates set NotBefore = Now() - Backdate.
//     Long-lived certificates set NotAfter = Now() + TTL - Backdate.
//     Short-lived certificates set NotAfter = Now() + TTL.
//     All certificates truncate NotAfter to the expiration date of the signer.
type PermissiveSigningPolicy struct {
	// TTL is used in certificate NotAfter calculation as described above.
	TTL time.Duration

	// Usages are the allowed usages of a certificate.
	Usages []certificatesv1.KeyUsage

	// Backdate is used in certificate NotBefore calculation as described above.
	Backdate time.Duration

	// Short is the duration used to determine if the lifetime of a certificate should be considered short.
	Short time.Duration

	// Now defaults to time.Now but can be stubbed for testing
	Now func() time.Time
}

func (p PermissiveSigningPolicy) apply(tmpl *x509.Certificate, signerNotAfter time.Time) error {
	var now time.Time
	if p.Now != nil {
		now = p.Now()
	} else {
		now = time.Now()
	}

	ttl := p.TTL

	usage, extUsages, err := keyUsagesFromStrings(p.Usages)
	if err != nil {
		return err
	}
	tmpl.KeyUsage = usage
	tmpl.ExtKeyUsage = extUsages

	tmpl.ExtraExtensions = nil
	tmpl.Extensions = nil
	tmpl.BasicConstraintsValid = true
	tmpl.IsCA = false

	tmpl.NotBefore = now.Add(-p.Backdate)

	if ttl < p.Short {
		// do not backdate the end time if we consider this to be a short lived certificate
		tmpl.NotAfter = now.Add(ttl)
	} else {
		tmpl.NotAfter = now.Add(ttl - p.Backdate)
	}

	if !tmpl.NotAfter.Before(signerNotAfter) {
		tmpl.NotAfter = signerNotAfter
	}

	if !tmpl.NotBefore.Before(signerNotAfter) {
		return fmt.Errorf("the signer has expired: NotAfter=%v", signerNotAfter)
	}

	if !now.Before(signerNotAfter) {
		return fmt.Errorf("refusing to sign a certificate that expired in the past: NotAfter=%v", signerNotAfter)
	}

	return nil
}

var keyUsageDict = map[certificatesv1.KeyUsage]x509.KeyUsage{
	certificatesv1.UsageSigning:           x509.KeyUsageDigitalSignature,
	certificatesv1.UsageDigitalSignature:  x509.KeyUsageDigitalSignature,
	certificatesv1.UsageContentCommitment: x509.KeyUsageContentCommitment,
	certificatesv1.UsageKeyEncipherment:   x509.KeyUsageKeyEncipherment,
	certificatesv1.UsageKeyAgreement:      x509.KeyUsageKeyAgreement,
	certificatesv1.UsageDataEncipherment:  x509.KeyUsageDataEncipherment,
	certificatesv1.UsageCertSign:          x509.KeyUsageCertSign,
	certificatesv1.UsageCRLSign:           x509.KeyUsageCRLSign,
	certificatesv1.UsageEncipherOnly:      x509.KeyUsageEncipherOnly,
	certificatesv1.UsageDecipherOnly:      x509.KeyUsageDecipherOnly,
}

var extKeyUsageDict = map[certificatesv1.KeyUsage]x509.ExtKeyUsage{
	certificatesv1.UsageAny:             x509.ExtKeyUsageAny,
	certificatesv1.UsageServerAuth:      x509.ExtKeyUsageServerAuth,
	certificatesv1.UsageClientAuth:      x509.ExtKeyUsageClientAuth,
	certificatesv1.UsageCodeSigning:     x509.ExtKeyUsageCodeSigning,
	certificatesv1.UsageEmailProtection: x509.ExtKeyUsageEmailProtection,
	certificatesv1.UsageSMIME:           x509.ExtKeyUsageEmailProtection,
	certificatesv1.UsageIPsecEndSystem:  x509.ExtKeyUsageIPSECEndSystem,
	certificatesv1.UsageIPsecTunnel:     x509.ExtKeyUsageIPSECTunnel,
	certificatesv1.UsageIPsecUser:       x509.ExtKeyUsageIPSECUser,
	certificatesv1.UsageTimestamping:    x509.ExtKeyUsageTimeStamping,
	certificatesv1.UsageOCSPSigning:     x509.ExtKeyUsageOCSPSigning,
	certificatesv1.UsageMicrosoftSGC:    x509.ExtKeyUsageMicrosoftServerGatedCrypto,
	certificatesv1.UsageNetscapeSGC:     x509.ExtKeyUsageNetscapeServerGatedCrypto,
}

// keyUsagesFromStrings will translate a slice of usage strings from the
// certificates API ("pkg/apis/certificates".KeyUsage) to x509.KeyUsage and
// x509.ExtKeyUsage types.
func keyUsagesFromStrings(usages []certificatesv1.KeyUsage) (x509.KeyUsage, []x509.ExtKeyUsage, error) {
	var keyUsage x509.KeyUsage
	var unrecognized []certificatesv1.KeyUsage
	extKeyUsages := make(map[x509.ExtKeyUsage]struct{})
	for _, usage := range usages {
		if val, ok := keyUsageDict[usage]; ok {
			keyUsage |= val
		} else if val, ok := extKeyUsageDict[usage]; ok {
			extKeyUsages[val] = struct{}{}
		} else {
			unrecognized = append(unrecognized, usage)
		}
	}

	var sorted sortedExtKeyUsage
	for eku := range extKeyUsages {
		sorted = append(sorted, eku)
	}
	sort.Sort(sorted)

	if len(unrecognized) > 0 {
		return 0, nil, fmt.Errorf("unrecognized usage values: %q", unrecognized)
	}

	return keyUsage, sorted, nil
}

type sortedExtKeyUsage []x509.ExtKeyUsage

func (s sortedExtKeyUsage) Len() int {
	return len(s)
}

func (s sortedExtKeyUsage) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortedExtKeyUsage) Less(i, j int) bool {
	return s[i] < s[j]
}
