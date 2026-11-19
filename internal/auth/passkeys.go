package auth

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kspears/postbox/internal/mail"
	"github.com/kspears/postbox/internal/secret"
	"github.com/kspears/postbox/internal/store"
)

// Outcomes of a passkey sign-in or enrolment other than success.
var (
	// ErrNoPRF means the passkey gave no PRF output, or not the one its password was sealed
	// with, and no POSTBOX_PASSKEY_KEY stands in for it, so it cannot unlock a password.
	ErrNoPRF = errors.New("passkey has no PRF output")
	// ErrUnknownPasskey means the passkey is not one of an account's, or did not verify.
	ErrUnknownPasskey = errors.New("unknown passkey")
	// ErrPasskeyRequired means the password was right but the account signs in with a passkey
	// only.
	ErrPasskeyRequired = errors.New("account requires a passkey")
	// ErrExpired means the ceremony's nonce is unknown, used or too old.
	ErrExpired = errors.New("passkey ceremony expired")
)

// NewPasswordNeeded is a passkey sign-in whose stored password the mail server refused. The
// person enters the new one, with Nonce, to finish.
type NewPasswordNeeded struct{ Nonce string }

func (NewPasswordNeeded) Error() string { return "the stored password was refused" }

// prfSalt is the one PRF input every passkey is asked to evaluate.
var prfSalt = []byte("postbox password key v1")

// ceremonyLife bounds how long a started enrolment or sign-in, or a sign-in waiting for a new
// password, stays usable.
const ceremonyLife = 2 * time.Minute

// ceremony is a started passkey exchange, held in memory under a random nonce.
type ceremony struct {
	expires time.Time
	data    webauthn.SessionData
	account string
	handle  []byte
	// A sign-in waiting for a new password: the passkey and the key its password is sealed with.
	passkey store.Passkey
	key     []byte
}

type ceremonies struct {
	mu   sync.Mutex
	byID map[string]ceremony
}

func (c *ceremonies) put(now time.Time, cer ceremony) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID = map[string]ceremony{}
	}
	for id, old := range c.byID {
		if !now.Before(old.expires) {
			delete(c.byID, id)
		}
	}
	nonce := rand.Text()
	cer.expires = now.Add(ceremonyLife)
	c.byID[nonce] = cer
	return nonce
}

// take returns the ceremony for nonce; keep leaves it usable for another try.
func (c *ceremonies) take(now time.Time, nonce string, keep bool) (ceremony, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cer, ok := c.byID[nonce]
	if !keep {
		delete(c.byID, nonce)
	}
	if !ok || !now.Before(cer.expires) {
		return ceremony{}, ErrExpired
	}
	return cer, nil
}

func (c *ceremonies) drop(nonce string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byID, nonce)
}

// newWebAuthn configures the relying party for the address people open Postbox at.
func newWebAuthn(public *url.URL) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          public.Hostname(),
		RPDisplayName: "Postbox",
		RPOrigins:     []string{public.Scheme + "://" + public.Host},
	})
}

// passkeyUser is an account as WebAuthn sees it.
type passkeyUser struct {
	account     string
	handle      []byte
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u passkeyUser) WebAuthnName() string                       { return u.account }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.account }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// user returns account with its passkeys. Every passkey of an account shares one user handle,
// so a passkey provider files them together; the first gets a random one.
func (a *Authenticator) user(ctx context.Context, account string) (passkeyUser, error) {
	rows, err := a.store.Passkeys(ctx, account)
	if err != nil {
		return passkeyUser{}, err
	}
	u := passkeyUser{account: account}
	for _, row := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(row.Credential, &c); err != nil {
			return passkeyUser{}, fmt.Errorf("passkey of %s: %w", account, err)
		}
		u.handle = row.UserHandle
		u.credentials = append(u.credentials, c)
	}
	if u.handle == nil {
		u.handle = make([]byte, 32)
		rand.Read(u.handle)
	}
	return u, nil
}

// EnrolOptions starts adding a passkey to account: what the browser's create() is given, and
// the nonce that finishes it.
func (a *Authenticator) EnrolOptions(ctx context.Context, account string) (string, *protocol.CredentialCreation, error) {
	u, err := a.user(ctx, account)
	if err != nil {
		return "", nil, err
	}
	exclude := make([]protocol.CredentialDescriptor, len(u.credentials))
	for i, c := range u.credentials {
		exclude[i] = c.Descriptor()
	}
	options, data, err := a.webauthn.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(exclude),
		webauthn.WithExtensions(webauthn.WithExtensionPRF(protocol.PRFValues{First: prfSalt})),
	)
	if err != nil {
		return "", nil, err
	}
	return a.pending.put(a.clock.Now(), ceremony{data: *data, account: account, handle: u.handle}), options, nil
}

// Enrol finishes adding a passkey: response is the browser's create() answer, and prf the
// passkey's PRF output for prfSalt. The account password is sealed under a key derived from prf
// and stored with the passkey. A passkey without PRF output (1Password, Bitwarden) uses
// POSTBOX_PASSKEY_KEY instead; with neither, nothing is stored and the error is ErrNoPRF.
func (a *Authenticator) Enrol(ctx context.Context, account, password, nonce string, response, prf []byte) (store.Passkey, error) {
	cer, err := a.pending.take(a.clock.Now(), nonce, false)
	if err != nil {
		return store.Passkey{}, err
	}
	if cer.account != account || cer.passkey.ID != nil {
		return store.Passkey{}, ErrExpired
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return store.Passkey{}, fmt.Errorf("%w: %v", ErrUnknownPasskey, err)
	}
	credential, err := a.webauthn.CreateCredential(passkeyUser{account: account, handle: cer.handle}, cer.data, parsed)
	if err != nil {
		return store.Passkey{}, fmt.Errorf("%w: %v", ErrUnknownPasskey, err)
	}
	lock := store.LockPRF
	if len(prf) == 0 {
		lock = store.LockServer
	}
	key, err := a.lockKey(lock, prf, credential.ID)
	if err != nil {
		return store.Passkey{}, err
	}
	sealed, err := secret.Seal(key, bound(credential.ID, account), password)
	if err != nil {
		return store.Passkey{}, err
	}
	saved, err := json.Marshal(credential)
	if err != nil {
		return store.Passkey{}, err
	}
	p := store.Passkey{
		ID: credential.ID, Account: account, UserHandle: cer.handle, Credential: saved,
		Sealed: sealed, Lock: lock, CreatedAt: a.clock.Now(),
	}
	return p, a.store.AddPasskey(ctx, p)
}

// SignInOptions starts a passkey sign-in: what the browser's get() is given, and the nonce
// that finishes it. No account is named; the passkey says whose it is.
func (a *Authenticator) SignInOptions() (string, *protocol.CredentialAssertion, error) {
	options, data, err := a.webauthn.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
		webauthn.WithAssertionExtensions(webauthn.WithExtensionPRF(protocol.PRFValues{First: prfSalt})),
	)
	if err != nil {
		return "", nil, err
	}
	return a.pending.put(a.clock.Now(), ceremony{data: *data}), options, nil
}

// SignIn finishes a passkey sign-in and returns the account and its password. It counts
// against the throttle like a password sign-in. When the mail server refuses the stored
// password the error is a NewPasswordNeeded.
func (a *Authenticator) SignIn(ctx context.Context, clientIP netip.Addr, nonce string, response, prf []byte) (string, string, error) {
	ip := throttleKey(clientIP)
	unlock := a.inFlight.lock(ip)
	defer unlock()

	if err := a.checkBlocked(ctx, "", ip); err != nil {
		return "", "", err
	}
	cer, err := a.pending.take(a.clock.Now(), nonce, false)
	if err != nil || cer.passkey.ID != nil || cer.account != "" {
		return "", "", ErrExpired
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return "", "", a.failPasskey(ctx, "", ip)
	}
	var row store.Passkey
	find := func(rawID, handle []byte) (webauthn.User, error) {
		p, err := a.store.PasskeyByID(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(p.UserHandle, handle) {
			return nil, ErrUnknownPasskey
		}
		var c webauthn.Credential
		if err := json.Unmarshal(p.Credential, &c); err != nil {
			return nil, err
		}
		row = p
		return passkeyUser{account: p.Account, handle: p.UserHandle, credentials: []webauthn.Credential{c}}, nil
	}
	credential, err := a.webauthn.ValidateDiscoverableLogin(find, cer.data, parsed)
	if err != nil {
		log.Printf("passkey sign-in: %v", err)
		return "", "", a.failPasskey(ctx, row.Account, ip)
	}
	account := row.Account
	if err := a.checkBlocked(ctx, account, ip); err != nil {
		return "", "", err
	}
	key, err := a.lockKey(row.Lock, prf, row.ID)
	if err != nil {
		return "", "", err
	}
	password, err := secret.Open(key, bound(row.ID, account), row.Sealed)
	if err != nil {
		return "", "", ErrNoPRF
	}
	if saved, err := json.Marshal(credential); err == nil {
		if err := a.store.UpdatePasskey(ctx, row.ID, saved, row.Sealed); err != nil {
			return "", "", err
		}
		row.Credential = saved
	}

	loginCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	c, err := a.server.Login(loginCtx, clientIP, account, password)
	if errors.Is(err, mail.ErrRefused) {
		return "", "", NewPasswordNeeded{Nonce: a.pending.put(a.clock.Now(), ceremony{account: account, passkey: row, key: key})}
	}
	if err != nil {
		return "", "", err
	}
	a.server.Logout(c)
	if err := a.store.ClearFailures(context.WithoutCancel(ctx), account, ip); err != nil {
		log.Printf("sign-in: clear failures: %v", err)
	}
	return account, password, nil
}

// NewPassword finishes a passkey sign-in whose stored password was refused: password is checked
// like a password sign-in and, when right, stored for the passkey in place of the old one.
func (a *Authenticator) NewPassword(ctx context.Context, clientIP netip.Addr, nonce, password string) (string, error) {
	// A wrong password may be corrected within the ceremony's life.
	cer, err := a.pending.take(a.clock.Now(), nonce, true)
	if err != nil || cer.passkey.ID == nil {
		return "", ErrExpired
	}
	if err := a.CheckPassword(ctx, clientIP, cer.account, password); err != nil {
		return "", err
	}
	a.pending.drop(nonce)
	sealed, err := secret.Seal(cer.key, bound(cer.passkey.ID, cer.account), password)
	if err != nil {
		return "", err
	}
	return cer.account, a.store.UpdatePasskey(ctx, cer.passkey.ID, cer.passkey.Credential, sealed)
}

// failPasskey records a passkey that did not verify. account is "" when the passkey named
// none Postbox knows.
func (a *Authenticator) failPasskey(ctx context.Context, account, ip string) error {
	if err := a.fail(ctx, account, ip); !errors.Is(err, ErrWrongPassword) {
		return err
	}
	return ErrUnknownPasskey
}

// lockKey derives the key a passkey's password is sealed with: from its PRF output, or from
// POSTBOX_PASSKEY_KEY for a passkey locked by the server. Either must be there.
func (a *Authenticator) lockKey(lock string, prf, credentialID []byte) ([]byte, error) {
	in := prf
	if lock == store.LockServer {
		in = a.serverKey
	}
	if len(in) != 32 {
		return nil, ErrNoPRF
	}
	return hkdf.Key(sha256.New, in, nil, "postbox passkey "+lock+" v1\x00"+string(credentialID), 32)
}

// bound ties a sealed password to its passkey and account.
func bound(credentialID []byte, account string) []byte {
	return []byte("postbox passkey v1\x00" + string(credentialID) + "\x00" + account)
}
