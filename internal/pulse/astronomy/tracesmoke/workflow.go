package tracesmoke

// RecommendationsBrowse is the safe Astronomy Shop Locust workflow mirrored by
// WebsiteUser.get_recommendations: GET /api/recommendations?productIds=…
// (read-only; no cart, checkout, payment, or email).
//
// Upstream: opentelemetry-demo src/load-generator/locustfile.py @ pin 3.1.0.
const RecommendationsBrowse = "recommendations-browse"

// DefaultProductID is a catalog ID from the pinned demo Locust product list.
const DefaultProductID = "OLJCESPC7Z"

// RecommendationsPath is the frontend API path for RecommendationsBrowse.
const RecommendationsPath = "/api/recommendations"

// ExpectedAppServices are OTEL service names that may appear on the
// recommendations browse path. Qualification requires at least two of these
// in the same trace (not merely in Jaeger's global service list).
//
// frontend-proxy / load-generator are infrastructure/traffic helpers and are
// not counted toward the multi-service application requirement.
var ExpectedAppServices = []string{
	"frontend",
	"recommendation",
	"product-catalog",
}

// KnownMappingServices is the minimal allowlist used to reject fabricated
// service names that do not exist in Astronomy Shop mapping.
var KnownMappingServices = map[string]struct{}{
	"frontend":        {},
	"frontend-web":    {},
	"frontend-proxy":  {},
	"recommendation":  {},
	"product-catalog": {},
	"currency":        {},
	"cart":            {},
	"checkout":        {},
	"payment":         {},
	"shipping":        {},
	"quote":           {},
	"ad":              {},
	"email":           {},
	"flagd":           {},
	"image-provider":  {},
	"load-generator":  {},
	"telemetry-docs":  {},
	"flagd-ui":        {},
}
