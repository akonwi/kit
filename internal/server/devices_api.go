package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/pairing"
	"golang.org/x/time/rate"
)

// Failed authentications and pairing redemptions are limited per remote
// address. Device tokens and pairing codes cannot be guessed; the limits bound
// the load one caller can cause without letting it affect other callers. A
// valid credential or code is never refused, so callers that share an address
// behind a proxy are not locked out by one of them.
const (
	failureInterval = 100 * time.Millisecond
	failureBurst    = 20
	// maxLimitedAddresses bounds the addresses tracked at once.
	maxLimitedAddresses = 4096
	// devicePurgeInterval is how often expired devices are purged.
	devicePurgeInterval = time.Hour
)

// failureLimiter limits failures per remote address.
type failureLimiter struct {
	mu        sync.Mutex
	addresses map[string]*rate.Limiter
}

func newFailureLimiter() *failureLimiter {
	return &failureLimiter{addresses: map[string]*rate.Limiter{}}
}

// fail records a failure from address and reports whether it is within the
// address's limit. IPv6 addresses share their /64, which one host can usually
// draw addresses from freely.
func (l *failureLimiter) fail(address string) bool {
	key := limitKey(address)
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter, ok := l.addresses[key]
	if !ok {
		if len(l.addresses) >= maxLimitedAddresses {
			l.evict()
		}
		limiter = rate.NewLimiter(rate.Every(failureInterval), failureBurst)
		l.addresses[key] = limiter
	}
	return limiter.Allow()
}

// evict forgets addresses whose limits have fully recovered, which no longer
// constrain them, or else one arbitrary address, so a caller cycling
// addresses cannot reset every other caller's limit at once.
func (l *failureLimiter) evict() {
	for key, limiter := range l.addresses {
		if limiter.Tokens() >= failureBurst {
			delete(l.addresses, key)
		}
	}
	for key := range l.addresses {
		if len(l.addresses) < maxLimitedAddresses {
			return
		}
		delete(l.addresses, key)
	}
}

// limitKey is the address a failure counts against: an IPv6 address's /64,
// or the address itself.
func limitKey(address string) string {
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.Unmap().Is6() {
		return address
	}
	prefix, err := parsed.WithZone("").Prefix(64)
	if err != nil {
		return address
	}
	return prefix.String()
}

// access authenticates requests with the daemon's local token or a device
// token, and serves pairing and device management.
type access struct {
	localToken string
	devices    *devices.Service
	pairing    *pairing.Registry
	failures   *failureLimiter
	// unauthenticated holds "METHOD path" for operations that need no
	// credential.
	unauthenticated map[string]bool
}

func newAccess(localToken string, deviceService *devices.Service, registry *pairing.Registry) *access {
	unauthenticated := map[string]bool{}
	for _, descriptor := range httpapi.Catalog() {
		if descriptor.Unauthenticated {
			unauthenticated[descriptor.Method+" "+descriptor.Path] = true
		}
	}
	return &access{
		localToken: localToken, devices: deviceService, pairing: registry,
		failures: newFailureLimiter(), unauthenticated: unauthenticated,
	}
}

type deviceContextKey struct{}

// requestDevice returns the device that authenticated a request, or "" for
// the local token.
func requestDevice(ctx context.Context) string {
	id, _ := ctx.Value(deviceContextKey{}).(string)
	return id
}

func remoteHost(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// skipsCredential reports whether request is for an operation that needs no
// credential and no instance. Its path must be unencoded, so it matches the
// route the mux serves.
func (a *access) skipsCredential(request *http.Request) bool {
	return request.URL.RawPath == "" && a.unauthenticated[request.Method+" "+request.URL.Path]
}

// unauthenticatedReadTimeout, a variable for tests, bounds how long a request without a valid
// credential may take to send its body, so a caller cannot hold connections
// open by sending one slowly.
var unauthenticatedReadTimeout = 10 * time.Second

// limitUnauthenticatedRead applies unauthenticatedReadTimeout to the rest of
// the request, including any unread body the server drains after a rejection.
func limitUnauthenticatedRead(writer http.ResponseWriter) {
	_ = http.NewResponseController(writer).SetReadDeadline(time.Now().Add(unauthenticatedReadTimeout))
}

// bearer returns the token of an RFC 6750 Authorization header.
func bearer(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	return token, ok && strings.EqualFold(scheme, "Bearer") && token != ""
}

// authenticate checks a request's credential. It returns the request to serve,
// bound to its device's revocation and expiry, or false after writing the
// rejection.
func (a *access) authenticate(writer http.ResponseWriter, request *http.Request) (*http.Request, func(), bool) {
	if a.skipsCredential(request) {
		limitUnauthenticatedRead(writer)
		return request, func() {}, true
	}
	token, ok := bearer(request.Header.Get("Authorization"))
	if ok && a.localToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.localToken)) == 1 {
		return request, func() {}, true
	}
	if ok && a.devices != nil && devices.IsToken(token) {
		device, err := a.devices.Authenticate(request.Context(), token, remoteHost(request))
		if err == nil {
			// A request ends when its device is revoked or its token
			// expires, including event streams opened before either.
			ctx, cancel := context.WithDeadline(context.WithValue(request.Context(), deviceContextKey{}, device.ID), device.Expires)
			stop := context.AfterFunc(device.Revoked, cancel)
			return request.WithContext(ctx), func() { stop(); cancel() }, true
		}
		if errors.Is(err, devices.ErrExpired) {
			writer.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUnauthorized, httpapi.ErrorCredentialExpired, "device token expired; pair this device again", nil))
			return nil, nil, false
		}
	}
	limitUnauthenticatedRead(writer)
	if !a.failures.fail(remoteHost(request)) {
		httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorRateLimited, "too many failed authentications", nil))
		return nil, nil, false
	}
	writer.Header().Set("WWW-Authenticate", "Bearer")
	httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUnauthorized, httpapi.ErrorUnauthorized, "unauthorized", nil))
	return nil, nil, false
}

func deviceInfo(record devices.Record, current string) protocol.DeviceInfo {
	return protocol.DeviceInfo{
		ID: record.ID, Name: record.Name,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano), LastUsedAt: record.LastUsedAt.UTC().Format(time.RFC3339Nano),
		LastAddress: record.LastAddress, ExpiresAt: record.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Current: record.ID == current,
	}
}

func registerDeviceRoutes(mux *http.ServeMux, a *access, logger *slog.Logger) {
	options := httpapi.ServeOptions{MaxRequestBytes: 4 << 10, WriteError: func(writer http.ResponseWriter, err error) {
		var apiError *httpapi.APIError
		if !errors.As(err, &apiError) {
			if logger != nil {
				logger.Error("device request failed", "error", err)
			}
			apiError = httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal error", nil)
		}
		httpapi.WriteError(writer, apiError)
	}}
	httpapi.Handle(mux, options, httpapi.RedeemPairing, func(ctx context.Context, _ httpapi.ServerPath, input protocol.RedeemPairingInput) (protocol.RedeemPairingResult, error) {
		if err := input.Validate(); err != nil {
			return protocol.RedeemPairingResult{}, httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, err.Error(), nil)
		}
		// A valid code is always honored; only failures count against the
		// caller's address.
		if err := a.pairing.Redeem(input.Code); err != nil {
			if !a.failures.fail(requestAddress(ctx)) {
				return protocol.RedeemPairingResult{}, httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorRateLimited, "too many pairing attempts", nil)
			}
			return protocol.RedeemPairingResult{}, httpapi.NewAPIError(http.StatusUnauthorized, httpapi.ErrorUnauthorized, "pairing code is invalid or expired", nil)
		}
		// The code is spent; issuing completes even if the client goes away.
		record, token, err := a.devices.Issue(context.WithoutCancel(ctx), input.DeviceName, requestAddress(ctx))
		if errors.Is(err, devices.ErrCapacity) {
			return protocol.RedeemPairingResult{}, httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, err.Error(), nil)
		}
		if err != nil {
			return protocol.RedeemPairingResult{}, err
		}
		return protocol.RedeemPairingResult{Token: token, Device: deviceInfo(record, record.ID)}, nil
	})
	httpapi.Handle(mux, options, httpapi.CreatePairing, func(context.Context, httpapi.ServerPath, httpapi.NoBody) (protocol.PairingCode, error) {
		code, expires, err := a.pairing.Issue(pairing.CommandLifetime)
		if err != nil {
			return protocol.PairingCode{}, err
		}
		return protocol.PairingCode{Code: code, ExpiresAt: expires.Format(time.RFC3339Nano)}, nil
	})
	httpapi.Handle(mux, options, httpapi.ListDevices, func(ctx context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (protocol.DeviceList, error) {
		current := requestDevice(ctx)
		records := a.devices.List()
		list := protocol.DeviceList{Devices: make([]protocol.DeviceInfo, 0, len(records))}
		for _, record := range records {
			list.Devices = append(list.Devices, deviceInfo(record, current))
		}
		return list, nil
	})
	httpapi.Handle(mux, options, httpapi.RevokeDevice, func(ctx context.Context, path httpapi.DevicePath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		// Revoking the requesting device ends this request's context; the
		// revocation itself must still complete.
		err := a.devices.Revoke(context.WithoutCancel(ctx), path.DeviceID)
		if errors.Is(err, devices.ErrNotFound) {
			return httpapi.NoBody{}, httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "device not found", nil)
		}
		return httpapi.NoBody{}, err
	})
}

type remoteAddressKey struct{}

// withRequestAddress records the request's remote host for handlers.
func withRequestAddress(request *http.Request) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), remoteAddressKey{}, remoteHost(request)))
}

func requestAddress(ctx context.Context) string {
	address, _ := ctx.Value(remoteAddressKey{}).(string)
	return address
}
