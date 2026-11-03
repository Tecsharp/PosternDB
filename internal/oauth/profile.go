// Package oauth signs users in with Google (OpenID Connect, authorization
// code flow with PKCE). It only proves who the person is; accounts, sessions
// and permissions stay in internal/clients and internal/auth.
package oauth

// Profile is what Google tells us about the person who just signed in.
type Profile struct {
	// Subject is Google's stable id for the account (the "sub" claim). The
	// email can change, the subject cannot, so it is what we link to.
	Subject       string
	Email         string
	Name          string
	EmailVerified bool
}
