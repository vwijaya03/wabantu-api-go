package tlsguard

import (
	"crypto/x509"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"runtime"
	"time"

	"encore.dev/rlog"
)

// Fixtures: offline eMudhra test chains from
// https://emudhra.we-have-a-problem.com/chains.zip plus the two roots they
// chain to. See fixtures/README.md for provenance and hashes.
//
//go:embed fixtures/*.pem
var fixtureFS embed.FS

// selfCheckTime is a fixed verification time inside every fixture's validity
// window, so the check keeps working after the test leaves expire (May 2027).
var selfCheckTime = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)

const selfCheckTimeout = 5 * time.Second

// Result is the outcome of the startup self-check (log-only).
type Result struct {
	GoVersion string
	// Stdlib: PATCHED  = crypto/x509.Verify rejected the eMudhra chain with
	//                    NoValidChains although its root was trusted (denylist present);
	//         VULNERABLE = Verify accepted the eMudhra chain;
	//         UNKNOWN  = any other outcome (error, timeout, panic).
	Stdlib string
	// SysRootEmSignECCG3: TRUSTED / NOT_TRUSTED / UNKNOWN — whether the system
	// root store still trusts "emSign ECC Root CA - G3" (cross-signer).
	SysRootEmSignECCG3 string
	// Guard: ACTIVE (DefaultTransport hooked, mode log|enforce, guard flags the
	// fixture chain), OFF (mode off) or INACTIVE (not hooked / self-test failed).
	Guard  string
	Mode   Mode
	Detail string
}

// RunSelfCheck runs the check with a timeout, recovers from any panic, makes
// no network calls, and logs one line. It never panics and never exits.
func RunSelfCheck() (res Result) {
	res = Result{GoVersion: runtime.Version(), Stdlib: "UNKNOWN", SysRootEmSignECCG3: "UNKNOWN", Guard: "INACTIVE"}
	done := make(chan Result, 1)
	go func() {
		r := res
		defer func() {
			if p := recover(); p != nil {
				r.Detail = fmt.Sprintf("panic: %v", p)
			}
			done <- r
		}()
		r = selfCheck(r)
	}()
	select {
	case res = <-done:
	case <-time.After(selfCheckTimeout):
		res.Detail = "timeout"
	}
	defer func() { _ = recover() }() // logging must never crash startup either
	rlog.Info("tlsguard self-check",
		"go_version", res.GoVersion,
		"emudhra_check", res.Stdlib,
		"emudhra_sysroot_emsign_ecc_g3", res.SysRootEmSignECCG3,
		"tlsguard", res.Guard,
		"tlsguard_mode", string(res.Mode),
		"detail", res.Detail)
	return res
}

func selfCheck(r Result) Result {
	r.Mode = CurrentMode()
	leaf, inters, err := loadChain("fixtures/emudhra-dv-g3b-chain.pem")
	if err != nil {
		r.Detail = err.Error()
		return r
	}
	roots := x509.NewCertPool()
	for _, f := range []string{"fixtures/root-emsign-ecc-root-ca-g3.pem", "fixtures/root-aaa-certificate-services.pem"} {
		certs, err := loadPEM(f)
		if err != nil {
			r.Detail = err.Error()
			return r
		}
		for _, c := range certs {
			roots.AddCert(c)
		}
	}
	interPool := x509.NewCertPool()
	for _, c := range inters {
		interPool.AddCert(c)
	}

	// 1) Plain stdlib: roots explicitly trusted, so only the Go denylist can reject.
	r.Stdlib = classifyStdlib(leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: interPool, CurrentTime: selfCheckTime,
	}))

	// 2) Does the system bundle still trust emSign ECC Root CA - G3? Verify the
	//    (non-denylisted) "emSign Root TLS CA - G3" cross cert against system roots.
	if sys, err := x509.SystemCertPool(); err == nil && len(inters) >= 2 {
		_, verr := inters[len(inters)-1].Verify(x509.VerifyOptions{
			Roots: sys, CurrentTime: selfCheckTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
		if verr == nil {
			r.SysRootEmSignECCG3 = "TRUSTED"
		} else {
			r.SysRootEmSignECCG3 = "NOT_TRUSTED"
		}
	}

	// 3) Guard: hooked + flags the known-bad chain (built by hand, since a
	//    patched stdlib would not return it).
	chain := append([]*x509.Certificate{leaf}, inters...)
	switch {
	case r.Mode == ModeOff:
		r.Guard = "OFF"
	case !DefaultTransportHooked():
		r.Guard = "INACTIVE"
		r.Detail = "http.DefaultTransport not hooked"
	case distrustedCert(chain) == nil:
		r.Guard = "INACTIVE"
		r.Detail = "guard did not flag fixture chain"
	default:
		r.Guard = "ACTIVE"
	}
	return r
}

func classifyStdlib(chains [][]*x509.Certificate, err error) string {
	if err == nil && len(chains) > 0 {
		return "VULNERABLE"
	}
	var cie x509.CertificateInvalidError
	if errors.As(err, &cie) && cie.Reason == x509.NoValidChains {
		return "PATCHED"
	}
	return "UNKNOWN"
}

func loadChain(name string) (*x509.Certificate, []*x509.Certificate, error) {
	certs, err := loadPEM(name)
	if err != nil {
		return nil, nil, err
	}
	if len(certs) < 2 {
		return nil, nil, fmt.Errorf("%s: want leaf + intermediates", name)
	}
	return certs[0], certs[1:], nil
}

func loadPEM(name string) ([]*x509.Certificate, error) {
	b, err := fixtureFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return parsePEM(b)
}

func parsePEM(b []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no certificates")
	}
	return out, nil
}
