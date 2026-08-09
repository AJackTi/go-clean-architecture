package httpapi

// Option customizes an item HTTP Handler.
type Option func(*Handler)

// WithMaxBodyBytes limits the encoded JSON request body accepted by POST
// /items.  Values less than one are ignored and restore the default limit.
func WithMaxBodyBytes(limit int64) Option {
	return func(h *Handler) {
		if limit > 0 {
			h.maxBodyBytes = limit
		}
	}
}
