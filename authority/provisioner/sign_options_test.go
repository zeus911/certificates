package provisioner

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"net/url"
	"testing"
	"time"
)

func Test_emailOnlyIdentity_Valid(t *testing.T) {
	uri, err := url.Parse("https://example.com/1.0/getUser")
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		req *x509.CertificateRequest
	}
	tests := []struct {
		name    string
		e       emailOnlyIdentity
		args    args
		wantErr bool
	}{
		{"ok", "name@smallstep.com", args{&x509.CertificateRequest{EmailAddresses: []string{"name@smallstep.com"}}}, false},
		{"DNSNames", "name@smallstep.com", args{&x509.CertificateRequest{DNSNames: []string{"foo.bar.zar"}}}, true},
		{"IPAddresses", "name@smallstep.com", args{&x509.CertificateRequest{IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}}}, true},
		{"URIs", "name@smallstep.com", args{&x509.CertificateRequest{URIs: []*url.URL{uri}}}, true},
		{"no-emails", "name@smallstep.com", args{&x509.CertificateRequest{EmailAddresses: []string{}}}, true},
		{"empty-email", "", args{&x509.CertificateRequest{EmailAddresses: []string{""}}}, true},
		{"multiple-emails", "name@smallstep.com", args{&x509.CertificateRequest{EmailAddresses: []string{"name@smallstep.com", "foo@smallstep.com"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.e.Valid(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("emailOnlyIdentity.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_commonNameValidator_Valid(t *testing.T) {
	type args struct {
		req *x509.CertificateRequest
	}
	tests := []struct {
		name    string
		v       commonNameValidator
		args    args
		wantErr bool
	}{
		{"ok", "foo.bar.zar", args{&x509.CertificateRequest{Subject: pkix.Name{CommonName: "foo.bar.zar"}}}, false},
		{"empty", "", args{&x509.CertificateRequest{Subject: pkix.Name{CommonName: ""}}}, true},
		{"wrong", "foo.bar.zar", args{&x509.CertificateRequest{Subject: pkix.Name{CommonName: "example.com"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.v.Valid(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("commonNameValidator.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_dnsNamesValidator_Valid(t *testing.T) {
	type args struct {
		req *x509.CertificateRequest
	}
	tests := []struct {
		name    string
		v       dnsNamesValidator
		args    args
		wantErr bool
	}{
		{"ok0", []string{}, args{&x509.CertificateRequest{DNSNames: []string{}}}, false},
		{"ok1", []string{"foo.bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"foo.bar.zar"}}}, false},
		{"ok2", []string{"foo.bar.zar", "bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"foo.bar.zar", "bar.zar"}}}, false},
		{"ok3", []string{"foo.bar.zar", "bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"bar.zar", "foo.bar.zar"}}}, false},
		{"fail1", []string{"foo.bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"bar.zar"}}}, true},
		{"fail2", []string{"foo.bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"bar.zar", "foo.bar.zar"}}}, true},
		{"fail3", []string{"foo.bar.zar", "bar.zar"}, args{&x509.CertificateRequest{DNSNames: []string{"foo.bar.zar", "zar.bar"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.v.Valid(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("dnsNamesValidator.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ipAddressesValidator_Valid(t *testing.T) {
	ip1 := net.IPv4(10, 3, 2, 1)
	ip2 := net.IPv4(10, 3, 2, 2)
	ip3 := net.IPv4(10, 3, 2, 3)

	type args struct {
		req *x509.CertificateRequest
	}
	tests := []struct {
		name    string
		v       ipAddressesValidator
		args    args
		wantErr bool
	}{
		{"ok0", []net.IP{}, args{&x509.CertificateRequest{IPAddresses: []net.IP{}}}, false},
		{"ok1", []net.IP{ip1}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip1}}}, false},
		{"ok2", []net.IP{ip1, ip2}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip1, ip2}}}, false},
		{"ok3", []net.IP{ip1, ip2}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip2, ip1}}}, false},
		{"fail1", []net.IP{ip1}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip2}}}, true},
		{"fail2", []net.IP{ip1}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip2, ip1}}}, true},
		{"fail3", []net.IP{ip1, ip2}, args{&x509.CertificateRequest{IPAddresses: []net.IP{ip1, ip3}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.v.Valid(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("ipAddressesValidator.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validityValidator_Valid(t *testing.T) {
	type fields struct {
		min time.Duration
		max time.Duration
	}
	type args struct {
		crt *x509.Certificate
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &validityValidator{
				min: tt.fields.min,
				max: tt.fields.max,
			}
			if err := v.Valid(tt.args.crt); (err != nil) != tt.wantErr {
				t.Errorf("validityValidator.Valid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
