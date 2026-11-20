package auth

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// ceremonyLife bounds how long a started enrolment or sign-in stays usable: long enough for a
// second prompt and a security key's PIN. A sign-in waiting for a new password keeps the key
// that opens a password in memory, so it lasts newPasswordLife.
const (
	ceremonyLife    = 5 * time.Minute
	newPasswordLife = 2 * time.Minute
)

// maxCeremonies bounds the ceremonies held at once: sign-in ones can be started by anyone. When
// the table is full, the ceremony nearest its expiry makes room, so a flood cannot hold the table
// and refuse everyone; to push out someone's ceremony mid-sign-in it must fill all of it in the
// seconds that takes, from 500 or more addresses (maxCeremoniesPerVisitor).
const maxCeremonies = 10_000

// maxCeremoniesPerVisitor bounds the open sign-in ceremonies one visitor may hold, so one client
// cannot use up the room for everyone else.
const maxCeremoniesPerVisitor = 20

// purgeEvery is how often expired ceremonies are swept out. A visitor's count includes ones
// expired since the last sweep.
const purgeEvery = time.Minute

// ErrVisitorBusy means this visitor already holds as many open sign-in ceremonies as it may.
var ErrVisitorBusy = errors.New("too many passkey ceremonies from this address")

// What a ceremony is for. A nonce finishes only a ceremony of its own kind.
type ceremonyKind int

const (
	enrolment ceremonyKind = iota + 1
	signIn
	newPassword
)

// ceremony is a started passkey exchange, held in memory under a random nonce.
type ceremony struct {
	kind    ceremonyKind
	expires time.Time
	data    webauthn.SessionData
	account string
	// A sign-in: the throttle key of the visitor who started it. Empty when not counted per visitor.
	visitor string
	// Enrolment: the account's user handle.
	handle []byte
	// A sign-in waiting for a new password: the passkey and the key its password is sealed with.
	passkeyID []byte
	key       []byte
}

type ceremonies struct {
	mu   sync.Mutex
	byID map[string]ceremony
	// byVisitor counts the ceremonies in byID per visitor, so a request need not scan them all.
	byVisitor map[string]int
	purged    time.Time
}

func (c *ceremonies) put(now time.Time, cer ceremony) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID, c.byVisitor = map[string]ceremony{}, map[string]int{}
	}
	if now.Sub(c.purged) >= purgeEvery || len(c.byID) >= maxCeremonies {
		for id, old := range c.byID {
			if !now.Before(old.expires) {
				c.remove(id)
			}
		}
		c.purged = now
	}
	if cer.visitor != "" && c.byVisitor[cer.visitor] >= maxCeremoniesPerVisitor {
		return "", ErrVisitorBusy
	}
	if len(c.byID) >= maxCeremonies {
		var first string
		for id, old := range c.byID {
			if first == "" || old.expires.Before(c.byID[first].expires) {
				first = id
			}
		}
		c.remove(first)
	}
	nonce := rand.Text()
	cer.expires = now.Add(ceremonyLife)
	if cer.kind == newPassword {
		cer.expires = now.Add(newPasswordLife)
		// It holds a key that opens a stored password: gone when it expires, not when the map
		// next fills.
		time.AfterFunc(newPasswordLife, func() { c.drop(nonce) })
	}
	c.byID[nonce] = cer
	if cer.visitor != "" {
		c.byVisitor[cer.visitor]++
	}
	return nonce, nil
}

// remove deletes the ceremony id and its visitor's count. c.mu must be held.
func (c *ceremonies) remove(id string) {
	cer, ok := c.byID[id]
	if !ok {
		return
	}
	delete(c.byID, id)
	if cer.visitor == "" {
		return
	}
	if c.byVisitor[cer.visitor]--; c.byVisitor[cer.visitor] <= 0 {
		delete(c.byVisitor, cer.visitor)
	}
}

// get returns the ceremony of kind for nonce and leaves it usable.
func (c *ceremonies) get(now time.Time, nonce string, kind ceremonyKind) (ceremony, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cer, ok := c.byID[nonce]
	if !ok || cer.kind != kind || !now.Before(cer.expires) {
		return ceremony{}, ErrExpired
	}
	return cer, nil
}

// take is get, and uses the ceremony up.
func (c *ceremonies) take(now time.Time, nonce string, kind ceremonyKind) (ceremony, error) {
	cer, err := c.get(now, nonce, kind)
	if err == nil {
		c.drop(nonce)
	}
	return cer, err
}

func (c *ceremonies) drop(nonce string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(nonce)
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
	options, data, err := a.webauthn.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(webauthn.Credentials(u.credentials).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionPRF(protocol.PRFValues{First: prfSalt})),
	)
	if err != nil {
		return "", nil, err
	}
	nonce, err := a.pending.put(a.clock.Now(), ceremony{kind: enrolment, data: *data, account: account, handle: u.handle})
	return nonce, options, err
}

// Enrol finishes adding a passkey: response is the browser's create() answer, and prf the
// passkey's PRF output for prfSalt. The account password is sealed under a key derived from prf
// and stored with the passkey. A passkey without PRF output (1Password, Bitwarden) uses
// POSTBOX_PASSKEY_KEY instead; with neither, nothing is stored and the error is ErrNoPRF.
func (a *Authenticator) Enrol(ctx context.Context, account, password, nonce string, response, prf []byte) (store.Passkey, error) {
	cer, err := a.pending.take(a.clock.Now(), nonce, enrolment)
	if err != nil || cer.account != account {
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
// that finishes it. No account is named; the passkey says whose it is. The ceremony counts
// against clientIP's share of the open ones.
func (a *Authenticator) SignInOptions(clientIP netip.Addr) (string, *protocol.CredentialAssertion, error) {
	options, data, err := a.webauthn.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
		webauthn.WithAssertionExtensions(webauthn.WithExtensionPRF(protocol.PRFValues{First: prfSalt})),
	)
	if err != nil {
		return "", nil, err
	}
	nonce, err := a.pending.put(a.clock.Now(), ceremony{kind: signIn, data: *data, visitor: throttleKey(clientIP)})
	return nonce, options, err
}

// SignIn finishes a passkey sign-in and returns the account and its password. It counts
// against the throttle like a password sign-in. When the mail server refuses the stored
// password the error is a NewPasswordNeeded.
func (a *Authenticator) SignIn(ctx context.Context, clientIP netip.Addr, nonce string, response, prf []byte) (string, string, error) {
	ip := throttleKey(clientIP)
	unlock := a.inFlight.lock(ip)
	defer unlock()

	cer, err := a.pending.take(a.clock.Now(), nonce, signIn)
	if err != nil {
		return "", "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return "", "", a.failPasskey(ctx, unknownPasskey(nil), ip)
	}
	// Until it verifies, a passkey's failures count against it, not an account: retrying one
	// removed from Postbox blocks only that passkey, while the per-IP limit counts them all.
	who := unknownPasskey(parsed.RawID)
	if err := a.checkBlocked(ctx, who, ip); err != nil {
		return "", "", err
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
		return "", "", a.failPasskey(ctx, cmp.Or(row.Account, who), ip)
	}
	account := row.Account
	if credential.Authenticator.CloneWarning {
		log.Printf("passkey sign-in: %s's passkey reported a lower sign count than before; it may have been copied", account)
	}
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
	// The counters and flags the passkey reported.
	saved, err := json.Marshal(credential)
	if err != nil {
		return "", "", err
	}
	if err := a.store.UpdatePasskeyCredential(ctx, row.ID, saved); err != nil {
		return "", "", err
	}

	loginCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	c, err := a.server.Login(loginCtx, clientIP, account, password)
	if errors.Is(err, mail.ErrRefused) {
		nonce, err := a.pending.put(a.clock.Now(), ceremony{kind: newPassword, account: account, passkeyID: row.ID, key: key})
		if err != nil {
			return "", "", err
		}
		return "", "", NewPasswordNeeded{Nonce: nonce}
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
	cer, err := a.pending.get(a.clock.Now(), nonce, newPassword)
	if err != nil {
		return "", err
	}
	if err := a.CheckPassword(ctx, clientIP, cer.account, password); err != nil {
		return "", err
	}
	a.pending.drop(nonce)
	sealed, err := secret.Seal(cer.key, bound(cer.passkeyID, cer.account), password)
	if err != nil {
		return "", err
	}
	return cer.account, a.store.UpdatePasskeySealed(ctx, cer.passkeyID, sealed)
}

// unknownPasskey is what a passkey's failures count against before it names an account.
func unknownPasskey(rawID []byte) string {
	return "passkey " + hex.EncodeToString(rawID)
}

// failPasskey records a passkey that did not verify, against its account, or against
// unknownPasskey when it named none Postbox knows.
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
