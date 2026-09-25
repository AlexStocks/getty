/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package getty

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var tlsTestRootCertificate = []byte(`-----BEGIN CERTIFICATE-----
MIIBiDCCAS+gAwIBAgIUMaJuA5AGTTBvqSWb4fhJCC7UY4wwCgYIKoZIzj0EAwIw
GjEYMBYGA1UEAwwPZ2V0dHktdGVzdC1yb290MB4XDTI2MDczMTIzMTYyN1oXDTM2
MDcyODIzMTYyN1owGjEYMBYGA1UEAwwPZ2V0dHktdGVzdC1yb290MFkwEwYHKoZI
zj0CAQYIKoZIzj0DAQcDQgAEWZNS+42M+wb2AmNunl7ccsdoaRYanWn1kgt5Rj7X
50hqE1aA8Wdl7dbbDmCwSrwLRNus1ebi2571N0XJNXn536NTMFEwHQYDVR0OBBYE
FJYJbIsdqMVkz65eVtuLmz41l4IjMB8GA1UdIwQYMBaAFJYJbIsdqMVkz65eVtuL
mz41l4IjMA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDRwAwRAIgX6EFP2GN
UF0MEbozG6tzqvrF1R8NUNEUEF4ThXnQMpMCIB/191gSjtjhiuDKu/pT5cCXe9ka
Wf17jc2sFoJ9DUsb
-----END CERTIFICATE-----`)

func TestClientTLSConfigBuilderMinimumVersion(t *testing.T) {
	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "client.crt")
	keyPath := filepath.Join(tempDir, "client.key")
	caPath := filepath.Join(tempDir, "ca.crt")
	for path, data := range map[string][]byte{
		certPath: WssServerCRT,
		keyPath:  WssServerKEY,
		caPath:   tlsTestRootCertificate,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	config, err := (&ClientTlsConfigBuilder{
		ClientKeyCertChainPath:        certPath,
		ClientPrivateKeyPath:          keyPath,
		ClientTrustCertCollectionPath: caPath,
	}).BuildTlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %d, want TLS 1.2 (%d)", config.MinVersion, tls.VersionTLS12)
	}
	if config.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify is true; certificate verification must stay enabled")
	}
	if config.RootCAs == nil {
		t.Fatal("RootCAs is nil; the configured trust collection was not loaded")
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("Certificates contains %d entries, want 1", len(config.Certificates))
	}
	expectedRootCAs := x509.NewCertPool()
	if !expectedRootCAs.AppendCertsFromPEM(tlsTestRootCertificate) {
		t.Fatal("failed to parse the expected root certificate")
	}
	if !config.RootCAs.Equal(expectedRootCAs) {
		t.Fatal("RootCAs does not contain the configured trust certificate")
	}
	expectedClientCertificate, _ := pem.Decode(WssServerCRT)
	if expectedClientCertificate == nil {
		t.Fatal("failed to decode the expected client certificate")
	}
	expectedRootCertificate, _ := pem.Decode(tlsTestRootCertificate)
	if expectedRootCertificate == nil {
		t.Fatal("failed to decode the expected root certificate")
	}
	if bytes.Equal(expectedClientCertificate.Bytes, expectedRootCertificate.Bytes) {
		t.Fatal("client and root certificate fixtures must be distinct")
	}
	if len(config.Certificates[0].Certificate) == 0 {
		t.Fatal("configured client certificate has an empty certificate chain")
	}
	if !bytes.Equal(config.Certificates[0].Certificate[0], expectedClientCertificate.Bytes) {
		t.Fatal("configured client certificate does not match the requested certificate")
	}
}

func TestClientTLSConfigBuilderRejectsInvalidTrustCollection(t *testing.T) {
	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "client.crt")
	keyPath := filepath.Join(tempDir, "client.key")
	caPath := filepath.Join(tempDir, "ca.crt")
	for path, data := range map[string][]byte{
		certPath: WssServerCRT,
		keyPath:  WssServerKEY,
		caPath:   []byte("not a certificate"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	config, err := (&ClientTlsConfigBuilder{
		ClientKeyCertChainPath:        certPath,
		ClientPrivateKeyPath:          keyPath,
		ClientTrustCertCollectionPath: caPath,
	}).BuildTlsConfig()
	if err == nil {
		t.Fatal("invalid trust collection returned nil error")
	}
	if config != nil {
		t.Fatal("config must be nil when BuildTlsConfig fails")
	}
}

// writeServerTLSFixtures lays out a server key pair plus a trust collection and
// returns their paths.
func writeServerTLSFixtures(t *testing.T) (certPath, keyPath, caPath string) {
	t.Helper()

	tempDir := t.TempDir()
	certPath = filepath.Join(tempDir, "server.crt")
	keyPath = filepath.Join(tempDir, "server.key")
	caPath = filepath.Join(tempDir, "ca.crt")
	for path, data := range map[string][]byte{
		certPath: WssServerCRT,
		keyPath:  WssServerKEY,
		caPath:   tlsTestRootCertificate,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return certPath, keyPath, caPath
}

// Regression test for #127: with a trust collection configured the server asked
// for a client certificate but never verified it (RequireAnyClientCert), so
// ServerTrustCertCollectionPath was dead configuration - any self-signed
// certificate completed the handshake and an operator who thought mTLS was on
// had none.
func TestServerTLSConfigBuilderVerifiesClientCertificate(t *testing.T) {
	certPath, keyPath, caPath := writeServerTLSFixtures(t)

	config, err := (&ServerTlsConfigBuilder{
		ServerKeyCertChainPath:        certPath,
		ServerPrivateKeyPath:          keyPath,
		ServerTrustCertCollectionPath: caPath,
	}).BuildTlsConfig()
	if err != nil {
		t.Fatal(err)
	}

	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert: ClientCAs is not consulted otherwise", config.ClientAuth)
	}
	if config.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify is true on a config that must verify clients")
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %d, want TLS 1.2 (%d)", config.MinVersion, tls.VersionTLS12)
	}
	expectedClientCAs := x509.NewCertPool()
	if !expectedClientCAs.AppendCertsFromPEM(tlsTestRootCertificate) {
		t.Fatal("failed to parse the expected trust collection")
	}
	if config.ClientCAs == nil || !config.ClientCAs.Equal(expectedClientCAs) {
		t.Fatal("ClientCAs does not contain the configured trust certificate")
	}
}

// Without a trust collection the server can only require that a client presents
// some certificate, but the protocol floor still applies.
func TestServerTLSConfigBuilderWithoutTrustCollection(t *testing.T) {
	certPath, keyPath, _ := writeServerTLSFixtures(t)

	config, err := (&ServerTlsConfigBuilder{
		ServerKeyCertChainPath: certPath,
		ServerPrivateKeyPath:   keyPath,
	}).BuildTlsConfig()
	if err != nil {
		t.Fatal(err)
	}

	if config.ClientAuth != tls.RequireAnyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAnyClientCert when no trust collection is configured", config.ClientAuth)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %d, want TLS 1.2 (%d)", config.MinVersion, tls.VersionTLS12)
	}
}

// The handshake test below issues its own CA, server and client certificates:
// the fixtures used by the config tests have no private key available, and a
// client certificate signed by their CA cannot be produced without one.
type testCertificateAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCertificateAuthority(t *testing.T) *testCertificateAuthority {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "getty test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca certificate: %v", err)
	}

	return &testCertificateAuthority{
		cert: cert,
		key:  key,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// issue signs a leaf certificate for cn. A server certificate gets the loopback
// address a client needs to verify it.
func (ca *testCertificateAuthority) issue(t *testing.T, cn string, server bool) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate %s key: %v", cn, err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if server {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("sign %s certificate: %v", cn, err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// writeTLSPair writes a certificate and its key where ServerTlsConfigBuilder can
// read them.
func writeTLSPair(t *testing.T, dir, name string, cert tls.Certificate) (certPath, keyPath string) {
	t.Helper()

	keyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("marshal %s key: %v", name, err)
	}
	certPath = filepath.Join(dir, name+".crt")
	keyPath = filepath.Join(dir, name+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	return certPath, keyPath
}

// TestServerTLSHandshakeVerifiesTheClientCertificate is the handshake level case
// #127 asks for: the tests above only assert the tls.Config fields, and before the
// fix a self-signed client certificate completed the handshake against a server
// with a configured trust collection, because RequireAnyClientCert demands a
// certificate without verifying it.
func TestServerTLSHandshakeVerifiesTheClientCertificate(t *testing.T) {
	ca := newTestCertificateAuthority(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caPath, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := writeTLSPair(t, dir, "server", ca.issue(t, "getty test server", true))

	srv := newServer(
		TCP_SERVER,
		WithLocalAddress("127.0.0.1:0"),
		WithServerSslEnabled(true),
		WithServerTlsConfigBuilder(&ServerTlsConfigBuilder{
			ServerKeyCertChainPath:        certPath,
			ServerPrivateKeyPath:          keyPath,
			ServerTrustCertCollectionPath: caPath,
		}),
	)
	handler := &MessageHandler{}
	srv.RunEventLoop(func(ss Session) error {
		ss.SetPkgHandler(&PackageHandler{})
		ss.SetEventListener(handler)

		return nil
	})
	defer srv.Close()
	addr := srv.Listener().Addr().String()

	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	clientConfig := func(cert tls.Certificate) *tls.Config {
		return &tls.Config{
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			// 1.2 on purpose: with 1.3 the client half of the handshake finishes
			// before the server rejects the certificate, and the failure only
			// shows up on the first read.
			MaxVersion: tls.VersionTLS12,
		}
	}

	trusted, err := tls.Dial("tcp", addr, clientConfig(ca.issue(t, "trusted client", false)))
	if err != nil {
		t.Fatalf("a client certificate issued by the trust collection was rejected: %v", err)
	}
	_ = trusted.Close()

	// The server rejects the certificate during the handshake and answers with an
	// alert, which ends Dial with an error. Do not settle for "the connection is
	// unusable afterwards": a read on a live connection just times out, and a
	// timeout would make this test green against the broken code as well.
	// A certificate issued by a CA the server does not trust is the realistic
	// case: it is a well formed chain, it simply is not ours.
	if untrusted, err := tls.Dial("tcp", addr, clientConfig(newTestCertificateAuthority(t).issue(t, "untrusted client", false))); err == nil {
		_ = untrusted.Close()
		t.Fatal("a client certificate from an untrusted CA completed the handshake against a configured trust collection")
	}
}
