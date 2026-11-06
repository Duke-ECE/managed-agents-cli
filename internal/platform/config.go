package platform

import "os"

const (
	defaultSupabaseURL = "https://uwtptxsstaaexdujhsmp.supabase.co"
	defaultAPIURL      = "https://api-managed-agent.colab.duke.edu"
	callbackURL        = "http://localhost:5173"
)

// DefaultAnonKey is a public Supabase browser key, also embedded in the console.
// It is copied from the frontend repository's public variable at release time.
const DefaultAnonKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6InV3dHB0eHNzdGFhZXhkdWpoc21wIiwicm9sZSI6ImFub24iLCJpYXQiOjE3ODQ5NTg2NDUsImV4cCI6MjEwMDUzNDY0NX0.7XTTzhOXGUv1H3XSBy78e-cu_8v5g5NpWyp2uN7yQ5Y"

type Config struct {
	SupabaseURL string
	AnonKey     string
	APIURL      string
	CallbackURL string
}

func FromEnvironment() Config {
	return Config{
		SupabaseURL: fallback(os.Getenv("MANAGED_AGENTS_SUPABASE_URL"), defaultSupabaseURL),
		AnonKey:     fallback(os.Getenv("MANAGED_AGENTS_SUPABASE_ANON_KEY"), DefaultAnonKey),
		APIURL:      fallback(os.Getenv("MANAGED_AGENTS_API_URL"), defaultAPIURL),
		CallbackURL: callbackURL,
	}
}

func fallback(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}
