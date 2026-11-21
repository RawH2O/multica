package cli

import "testing"

func TestReleaseSourceIsFixedToFork(t *testing.T) {
	t.Setenv("MULTICA_RELEASE_REPOSITORY", "example-owner/example-repo")
	t.Setenv("MULTICA_RELEASE_API_BASE_URL", "")

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

func TestReleaseMirrorsKeepForkRepositoryAndAssetNames(t *testing.T) {
	t.Setenv("MULTICA_RELEASE_API_BASE_URL", "https://mirror.example/api/")
	t.Setenv("MULTICA_RELEASE_DOWNLOAD_BASE_URL", "https://mirror.example/assets/")
	if got, want := releaseAPIBaseURL(), "https://mirror.example/api/repos/RawH2O/multica/releases"; got != want {
		t.Fatalf("release API = %q, want %q", got, want)
	}
	if got, want := releaseAssetDownloadURL("checksums.txt", "v1.2.3", "fallback"), "https://mirror.example/assets/v1.2.3/checksums.txt"; got != want {
		t.Fatalf("asset URL = %q, want %q", got, want)
	}
}
