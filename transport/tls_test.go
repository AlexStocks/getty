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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
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
