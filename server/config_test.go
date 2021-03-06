// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/apigee/apigee-remote-service-envoy/testutil"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
)

const (
	allConfigOptions = `
global:
  temp_dir: /tmp/apigee-istio
  keep_alive_max_connection_age: 10m
  api_address: :5000
  metrics_address: :5001
  tls:
    cert_file: tls.crt
    key_file: tls.key
tenant:
  internal_api: https://istioservices.apigee.net/edgemicro
  remote_service_api: https://org-test.apigee.net/remote-service
  org_name: org
  env_name: env
  key: mykey
  secret: mysecret
  client_timeout: 30s
  allow_unverified_ssl_cert: false
products:
  refresh_rate: 2m
analytics:
  legacy_endpoint: false
  file_limit: 1024
  send_channel_size: 10
  collection_interval: 10s
  fluentd_endpoint: apigee-udca-myorg-test.apigee.svc.cluster.local:20001
  tls:
    ca_file: /opt/apigee/tls/ca.crt
    cert_file: /opt/apigee/tls/tls.crt
    key_file: /opt/apigee/tls/tls.key
    allow_unverified_ssl_cert: false
auth:
  api_key_claim: claim
  api_key_cache_duration: 30m
  api_key_header: x-api-key
  target_header: :authority
  reject_unauthorized: true
  jwks_poll_interval: 0s`

	configMapConfigKey = "config.yaml"
)

func TestPlatformDetect(t *testing.T) {
	// OPDK
	config := &Config{
		Tenant: TenantConfig{
			InternalAPI: "x",
		},
	}
	if config.IsGCPManaged() {
		t.Fatalf("expected !config.isGCPExperience")
	}
	if config.IsApigeeManaged() {
		t.Fatalf("expected !config.IsApigeeManaged")
	}
	if !config.IsOPDK() {
		t.Fatalf("expected config.IsOPDK")
	}

	// Legacy SaaS
	config.Tenant.InternalAPI = LegacySaaSInternalBase
	if config.IsGCPManaged() {
		t.Fatalf("expected !config.isGCPExperience")
	}
	if !config.IsApigeeManaged() {
		t.Fatalf("expected config.IsApigeeManaged")
	}
	if config.IsOPDK() {
		t.Fatalf("expected !config.IsOPDK")
	}

	// Legacy SaaS
	config.Tenant.InternalAPI = LegacySaaSInternalBase
	if config.IsGCPManaged() {
		t.Fatalf("expected !config.isGCPExperience")
	}
	if !config.IsApigeeManaged() {
		t.Fatalf("expected config.IsApigeeManaged")
	}
	if config.IsOPDK() {
		t.Fatalf("expected !config.IsOPDK")
	}

	// GCP
	config.Tenant.InternalAPI = ""
	if !config.IsGCPManaged() {
		t.Fatalf("expected config.isGCPExperience")
	}
	if config.IsApigeeManaged() {
		t.Fatalf("expected !config.IsApigeeManaged")
	}
	if config.IsOPDK() {
		t.Fatalf("expected !config.IsOPDK")
	}

}

func TestHybridSingleFile(t *testing.T) {
	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	const config = `
    tenant:
      remote_service_api: https://org-test.apigee.net/remote-service
      org_name: org
      env_name: env
    analytics:
      fluentd_endpoint: apigee-udca-myorg-test.apigee.svc.cluster.local:20001`
	configCRD := makeConfigCRD(config)
	policySecretCRD, err := makePolicySecretCRD()
	if err != nil {
		t.Fatal(err)
	}
	analyticsSecretCRD, err := makeAnalyaticsSecretCRD()
	if err != nil {
		t.Fatal(err)
	}
	configMapYAML, err := makeYAML(configCRD, policySecretCRD, analyticsSecretCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := c.Load(tf.Name(), "xxx", "xxx"); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Tenant.PrivateKeyID, "my kid")
}

func TestMultifileConfig(t *testing.T) {
	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	configCRD, secretCRD, _, err := makeCRDs()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tf.WriteString(configCRD.Data[configMapConfigKey]); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	secretDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(secretDir)

	for k, v := range secretCRD.Data {
		data, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile(path.Join(secretDir, k), data, os.ModePerm); err != nil {
			t.Fatal(err)
		}
	}

	c := DefaultConfig()
	if err := c.Load(tf.Name(), secretDir, ""); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Tenant.PrivateKeyID, "my kid")
}

func TestIncompletePolicySecret(t *testing.T) {
	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	configCRD, policySecretCRD, _, err := makeCRDs()
	if err != nil {
		t.Fatal(err)
	}

	// remove the JWKS
	delete(policySecretCRD.Data, SecretJWKSKey)

	configMapYAML, err := makeYAML(configCRD, policySecretCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := c.Load(tf.Name(), "", ""); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Tenant.PrivateKeyID, "")
}

func TestLoadOrders(t *testing.T) {
	configCRD, policySecretCRD, analyticsSecretCRD, err := makeCRDs()
	if err != nil {
		t.Fatal(err)
	}

	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	// put ConfigMap in the end
	configMapYAML, err := makeYAML(policySecretCRD, analyticsSecretCRD, configCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := c.Load(tf.Name(), "", ""); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Tenant.PrivateKeyID, "my kid")

	tf, err = ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	// put ConfigMap in the middle
	configMapYAML, err = makeYAML(policySecretCRD, configCRD, analyticsSecretCRD)
	if err != nil {
		t.Fatal(err)
	}

	err = tf.Truncate(0) // re-create the yaml file
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c = DefaultConfig()
	if err := c.Load(tf.Name(), "", ""); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Tenant.PrivateKeyID, "my kid")
}

func TestLoadLegacyConfig(t *testing.T) {
	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	configCRD := makeConfigCRD(allConfigOptions)
	secretCRD, err := makePolicySecretCRD()
	if err != nil {
		t.Fatal(err)
	}
	configMapYAML, err := makeYAML(configCRD, secretCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c := &Config{}
	if err := c.Load(tf.Name(), "xxx", ""); err != nil {
		t.Fatal(err)
	}

	equal(t, c.Global.Namespace, "apigee")
	equal(t, c.Global.TempDir, "/tmp/apigee-istio")
}

func TestLoadAnalytics(t *testing.T) {
	const config = `
tenant:
  remote_service_api: https://org-test.apigee.net/remote-service
  org_name: org
  env_name: env`

	configCRD := makeConfigCRD(config)

	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	// put ConfigMap in the end
	configMapYAML, err := makeYAML(configCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	credDir, err := ioutil.TempDir("", "analytics-secret")
	if err != nil {
		t.Fatalf("%v", err)
	}
	credFile := path.Join(credDir, ServiceAccount)
	if err := ioutil.WriteFile(credFile, fakeServiceAccount(), 0644); err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(credDir)

	c := DefaultConfig()
	if err := c.Load(tf.Name(), "", credDir); err != nil {
		t.Error(err)
	}

	c = DefaultConfig()
	err = c.Load(tf.Name(), "", credFile)

	wantErrs := []string{
		"tenant.internal_api or tenant.analytics.fluentd_endpoint is required if no service account",
	}
	merr := err.(*multierror.Error)
	if merr.Len() != len(wantErrs) {
		t.Fatalf("got %d errors, want: %d, errors: %s", merr.Len(), len(wantErrs), merr)
	}

	errs := merr.Errors
	for i, e := range errs {
		equal(t, e.Error(), wantErrs[i])
	}
}

func TestInvalidConfig(t *testing.T) {
	tf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	// a bad simple config
	if _, err := tf.WriteString("not a good yaml"); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c := &Config{}
	if err := c.Load(tf.Name(), "", ""); err == nil {
		t.Error("should have gotten error")
	}

	tf, err = ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())

	configCRD := makeConfigCRD("not a good yaml")
	configMapYAML, err := makeYAML(configCRD)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tf.WriteString(configMapYAML); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	c = &Config{}
	if err := c.Load(tf.Name(), "", ""); err == nil {
		t.Error("should have gotten error")
	}
}

func makeConfigCRD(config string) *ConfigMapCRD {
	data := map[string]string{configMapConfigKey: config}
	return &ConfigMapCRD{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: Metadata{
			Name:      "apigee-remote-service-envoy",
			Namespace: "apigee",
		},
		Data: data,
	}
}

func TestValidate(t *testing.T) {
	c := &Config{}
	err := c.Validate()
	if err == nil {
		t.Fatal("should have gotten errors")
	}

	wantErrs := []string{
		"tenant.remote_service_api is required",
		"tenant.internal_api or tenant.analytics.fluentd_endpoint is required if no service account",
		"tenant.org_name is required",
		"tenant.env_name is required",
	}
	merr := err.(*multierror.Error)
	if merr.Len() != len(wantErrs) {
		t.Fatalf("got %d errors, want: %d, errors: %s", merr.Len(), len(wantErrs), merr)
	}

	errs := merr.Errors
	for i, e := range errs {
		equal(t, e.Error(), wantErrs[i])
	}
}

func TestValidateTLS(t *testing.T) {
	config := DefaultConfig()
	config.Tenant = TenantConfig{
		InternalAPI:      "http://localhost/remote-service",
		RemoteServiceAPI: "http://localhost/remote-service",
		OrgName:          "org",
		EnvName:          "env",
		Key:              "key",
		Secret:           "secret",
	}

	opts := [][]string{
		{"x", "", "x", "", ""},
		{"", "x", "", "x", ""},
		{"", "x", "", "", "x"},
		{"", "x", "x", "", "x"},
		{"", "x", "", "x", "x"},
		{"", "x", "x", "x", ""},
	}

	for i, o := range opts {
		t.Logf("round %d", i)
		config.Global.TLS.CertFile = o[0]
		config.Global.TLS.KeyFile = o[1]
		config.Analytics.TLS.CAFile = o[2]
		config.Analytics.TLS.CertFile = o[3]
		config.Analytics.TLS.KeyFile = o[4]

		err := config.Validate()
		if err == nil {
			t.Fatal("should have gotten errors")
		}
		wantErrs := []string{
			"global.tls.cert_file and global.tls.key_file are both required if either are present",
			"all analytics.tls options are required if any are present",
		}
		merr := err.(*multierror.Error)
		if merr.Len() != len(wantErrs) {
			t.Fatalf("got %d errors, want: %d, errors: %s", merr.Len(), len(wantErrs), merr)
		}

		errs := merr.Errors
		for i, e := range errs {
			equal(t, e.Error(), wantErrs[i])
		}
	}
}

func makePolicySecretCRD() (*SecretCRD, error) {
	kid := "my kid"
	privateKey, jwksBuf, err := testutil.GenerateKeyAndJWKs(kid)
	if err != nil {
		return nil, err
	}
	pkBytes := pem.EncodeToMemory(&pem.Block{Type: PEMKeyType, Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	props := map[string]string{SecretPropsKIDKey: kid}
	propsBuf := new(bytes.Buffer)
	if err := WriteProperties(propsBuf, props); err != nil {
		return nil, err
	}

	data := map[string]string{
		SecretJWKSKey:    base64.StdEncoding.EncodeToString(jwksBuf),
		SecretPrivateKey: base64.StdEncoding.EncodeToString(pkBytes),
		SecretPropsKey:   base64.StdEncoding.EncodeToString(propsBuf.Bytes()),
	}

	return &SecretCRD{
		APIVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Metadata: Metadata{
			Name:      "org-env-policy-secret",
			Namespace: "apigee",
		},
		Data: data,
	}, nil
}

func makeAnalyaticsSecretCRD() (*SecretCRD, error) {
	data := map[string]string{
		ServiceAccount: base64.StdEncoding.EncodeToString([]byte(`{"type": "service_account"}`)),
	}

	return &SecretCRD{
		APIVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Metadata: Metadata{
			Name:      "org-env-analytics-secret",
			Namespace: "apigee",
		},
		Data: data,
	}, nil
}

func makeCRDs() (configCRD *ConfigMapCRD, policySecretCRD, analyticsSecretCRD *SecretCRD, err error) {
	const config = `
tenant:
  remote_service_api: https://org-test.apigee.net/remote-service
  org_name: org
  env_name: env
analytics:
  fluentd_endpoint: apigee-udca-myorg-test.apigee.svc.cluster.local:20001`
	configCRD = makeConfigCRD(config)
	policySecretCRD, err = makePolicySecretCRD()
	if err != nil {
		return nil, nil, nil, err
	}
	analyticsSecretCRD, err = makeAnalyaticsSecretCRD()
	if err != nil {
		return nil, nil, nil, err
	}
	return configCRD, policySecretCRD, analyticsSecretCRD, nil
}

func makeYAML(crds ...interface{}) (string, error) {
	var yamlBuffer bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&yamlBuffer)
	yamlEncoder.SetIndent(2)
	for _, crd := range crds {
		if err := yamlEncoder.Encode(crd); err != nil {
			return "", err
		}
	}
	return yamlBuffer.String(), nil
}

func equal(t *testing.T, got, want string) {
	if got != want {
		t.Errorf("got: '%s', want: '%s'", got, want)
	}
}
