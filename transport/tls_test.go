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
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestClientTLSConfigBuilderMinimumVersion(t *testing.T) {
	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "client.crt")
	keyPath := filepath.Join(tempDir, "client.key")
	caPath := filepath.Join(tempDir, "ca.crt")
	for path, data := range map[string][]byte{
		certPath: WssServerCRT,
		keyPath:  WssServerKEY,
		caPath:   WssClientCRT,
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
