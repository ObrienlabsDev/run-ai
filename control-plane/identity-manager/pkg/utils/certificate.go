/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package utils

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

const (
	CERTIFICATE       = "CERTIFICATE"
	BEGIN_CERTIFICATE = "BEGIN " + CERTIFICATE
	END_CERTIFICATE   = "END " + CERTIFICATE
)

// ValidateSigningCertificate parses a SAML IdP signing certificate and verifies that it is signed.
// A self-signed certificate is accepted when its signature verifies with its own public key.
// A public certificate is accepted when it chains to a CA in the system trust store.
// A leaf outside its validity window is rejected.
// PEM values may include further certificates as intermediates for that chain.
// Any other value may list several certificates separated by commas; each is validated on its own.
func ValidateSigningCertificate(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if strings.Contains(trimmed, BEGIN_CERTIFICATE) {
		return validateSigningCertificate(trimmed, nil)
	}
	for _, part := range strings.Split(trimmed, ",") {
		if err := validateSigningCertificate(part, nil); err != nil {
			return err
		}
	}
	return nil
}

func validateSigningCertificate(raw string, roots *x509.CertPool) error {
	certs, err := parseSigningCertificates(raw)
	if err != nil {
		return err
	}
	leaf := certs[0]
	if leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil {
		return leafCurrentlyValid(leaf)
	}
	if err := verifyPublicCertificate(leaf, certs[1:], roots); err != nil {
		return fmt.Errorf("signing certificate is not signed: %w: %w", ErrUnsignedCertificate, err)
	}
	return leafCurrentlyValid(leaf)
}

func leafCurrentlyValid(leaf *x509.Certificate) error {
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return fmt.Errorf("signing certificate is not signed: %w", ErrUnsignedCertificate)
	}
	return nil
}

func verifyPublicCertificate(leaf *x509.Certificate, intermediates []*x509.Certificate, roots *x509.CertPool) error {
	if roots == nil {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			return fmt.Errorf("system trust store is unavailable")
		}
		roots = pool
	}
	extra := x509.NewCertPool()
	for _, intermediate := range intermediates {
		extra.AddCert(intermediate)
	}
	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: extra,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	return err
}

func parseSigningCertificates(raw string) ([]*x509.Certificate, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("signing certificate is required: %w", ErrUnsignedCertificate)
	}
	ders, err := decodeCertificates(trimmed)
	if err != nil {
		return nil, fmt.Errorf("signing certificate is invalid: %w: %w", ErrUnsignedCertificate, err)
	}
	if len(ders) == 0 {
		return nil, fmt.Errorf("signing certificate is invalid: %w", ErrUnsignedCertificate)
	}
	certs := make([]*x509.Certificate, 0, len(ders))
	for _, der := range ders {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("signing certificate is invalid: %w: %w", ErrUnsignedCertificate, err)
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func decodeCertificates(raw string) ([][]byte, error) {
	if strings.Contains(raw, BEGIN_CERTIFICATE) {
		var ders [][]byte
		rest := []byte(raw)
		for {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != CERTIFICATE {
				return nil, fmt.Errorf("pem block is not a certificate")
			}
			ders = append(ders, block.Bytes)
		}
		if len(ders) == 0 {
			return nil, fmt.Errorf("pem block is not a certificate")
		}
		return ders, nil
	}

	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, raw)
	der, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		der, err = base64.RawStdEncoding.DecodeString(compact)
	}
	if err != nil {
		return nil, err
	}
	return [][]byte{der}, nil
}
