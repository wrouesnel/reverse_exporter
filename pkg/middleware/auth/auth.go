package auth

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	"github.com/wrouesnel/reverse_exporter/pkg/config"
	"go.uber.org/zap"

	"github.com/shaj13/go-guardian/v2/auth/strategies/basic"
)

var (
	ErrInvalidUser        = errors.New("Invalid user")
	ErrInvalidCredentials = errors.New("Invalid credentials")
)

// basicValidator generates a basic auth validator function from the supplied map.
//nolint:unparam
func basicValidator(userMap map[string]map[string]struct{}) (basic.AuthenticateFunc, error) {
	return func(ctx context.Context, r *http.Request, username, password string) (auth.Info, error) {
		storedPasswords, ok := userMap[username]
		if !ok {
			return nil, ErrInvalidUser
		}
		if _, ok = storedPasswords[password]; ok {
			return auth.NewDefaultUser(username, username, nil, nil), nil
		}
		return nil, ErrInvalidCredentials
	}, nil
}

func passthru(next http.Handler) http.HandlerFunc {
	zap.L().With(zap.String("subsystem", "auth")).Info("Authentication not configured")
	return func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	}
}

// SetupAuthHandler returns a handler which implements the given AuthConfig object.
func SetupAuthHandler(config *config.AuthConfig, next http.Handler) (http.HandlerFunc, error) {
	if config == nil {
		return passthru(next), nil
	}

	l := zap.L().With(zap.String("subsystem", "auth"))

	if len(config.BasicAuthCredentials) == 0 {
		return passthru(next), nil
	}

	l.Info("Configuring basic authentication")
	authMap := map[string]map[string]struct{}{}
	for _, credential := range config.BasicAuthCredentials {
		if _, ok := authMap[credential.Username]; !ok {
			authMap[credential.Username] = make(map[string]struct{})
		}
		authMap[credential.Username][credential.Password] = struct{}{}
	}

	validator, err := basicValidator(authMap)
	if err != nil {
		return nil, errors.Wrap(err, "basic validator configuration failed")
	}

	strategy := union.New(basic.New(validator))

	return func(w http.ResponseWriter, r *http.Request) {
		l.Debug("Authentication Middleware")
		user, err := strategy.Authenticate(r.Context(), r)

		switch {
		case errors.Is(err, ErrInvalidUser), errors.Is(err, ErrInvalidCredentials):
			l.Debug("Authentication Failed", zap.Error(err))
			code := http.StatusUnauthorized
			http.Error(w, http.StatusText(code), code)
			return
		}

		l.Debug("Authentication Success", zap.String("user", user.GetUserName()))
		next.ServeHTTP(w, r)
	}, nil
}
