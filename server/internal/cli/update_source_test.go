package cli

import "testing"

func TestReleaseSourceIsFixedToFork(t *testing.T) {
	t.Setenv("MULTICA_RELEASE_REPOSITORY", "example-owner/example-repo")

	if got, want := ReleaseRepository(), DefaultReleaseRepository; got != want {
		t.Fatalf("ReleaseRepository() = %q, want fixed %q", got, want)
	}
	if got, want := releaseAPIBaseURL(), "https://api.github.com/repos/RawH2O/multica/releases"; got != want {
		t.Fatalf("release API base URL = %q, want %q", got, want)
	}
	if got, want := LatestReleaseURL(), "https://github.com/RawH2O/multica/releases/latest"; got != want {
		t.Fatalf("latest release URL = %q, want %q", got, want)
	}
}
