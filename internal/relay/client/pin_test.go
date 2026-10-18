package client

import (
	"crypto/sha256"
	"testing"
)

func TestPinLeafChecksOnlyTheLeafCertificate(t *testing.T) {
	relayCert := []byte("relay certificate DER")
	attackerCert := []byte("attacker certificate DER")
	sum := sha256.Sum256(relayCert)
	verify := pinLeaf(sum[:])

	cases := []struct {
		name    string
		chain   [][]byte
		wantErr bool
	}{
		{"pinned leaf", [][]byte{relayCert}, false},
		{"pinned leaf with extra chain cert", [][]byte{relayCert, attackerCert}, false},
		// A MITM can append the relay's public certificate after its own
		// leaf; only the leaf is backed by the handshake signature.
		{"pinned cert appended after another leaf", [][]byte{attackerCert, relayCert}, true},
		{"other leaf", [][]byte{attackerCert}, true},
		{"no certificates", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verify(tc.chain, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("verify err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
