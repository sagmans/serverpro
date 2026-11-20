// Package releasecontract_test verifies the release workflow structure,
// action pins, caching policy, dependency graph, and publication invariants
// directly against parsed YAML so format-only edits never break checks while
// security or gate regressions fail closed.
package releasecontract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	releaseWorkflowFile    = "release.yml"
	releaseTagGlob         = "v*"
	releaseUbuntuRunner    = "ubuntu-24.04"
	releaseMiseVersion     = "2026.10.23"
	releaseUVPython        = "3.14.10"
	releaseMiseTimeoutMin  = 5
	fullChainArtifactDir   = ".artifacts/full-chain-e2e"
	fullChainArtifactName  = "full-chain-e2e-failure"
	fullChainRetentionDays = "3"

	reviewedReleaseLdflags = `-ldflags "-s -w -buildid= -X github.com/sagmans/serverpro/internal/cli.Version=${GITHUB_REF_NAME}"`

	// reviewedReleaseBuildRun is the canonical build script. Comparing the whole
	// step text fail-closed is deliberate: parsing shell/Go flags proved
	// unbounded, so any build change must update this reviewed string.
	reviewedReleaseBuildRun = "set -eu\n" +
		"mkdir -p out\n" +
		"go build \\\n" +
		"  -trimpath \\\n" +
		"  -buildvcs=false \\\n" +
		"  " + reviewedReleaseLdflags + " \\\n" +
		"  -o out/serverpro \\\n" +
		"  ./cmd/serverpro\n"
)

var releaseTargets = []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"}

var reviewedActionPins = map[string]string{
	"actions/checkout":                "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
	"actions/setup-go":                "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
	"jdx/mise-action":                 "jdx/mise-action@6be3c7ed4c21d4f76b208d351d95aac273ebb76d",
	"actions/upload-artifact":         "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9",
	"actions/download-artifact":       "actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333",
	"anchore/sbom-action":             "anchore/sbom-action@66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c",
	"actions/attest-build-provenance": "actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8",
	"actions/attest":                  "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6",
}

var reviewedActionCounts = map[string]int{
	"actions/checkout":                6,
	"actions/setup-go":                3,
	"jdx/mise-action":                 1,
	"actions/upload-artifact":         3,
	"actions/download-artifact":       2,
	"anchore/sbom-action":             1,
	"actions/attest-build-provenance": 1,
	"actions/attest":                  1,
}

var reviewedJobNeeds = map[string][]string{
	"check":          nil,
	"full-chain-e2e": nil,
	"validate":       nil,
	"build":          {"check", "full-chain-e2e", "validate"},
	"package":        {"build"},
	"release":        {"check", "full-chain-e2e", "package", "validate"},
}

var reviewedJobPermissions = map[string]map[string]string{
	"check":          nil,
	"full-chain-e2e": nil,
	"validate":       nil,
	"build":          nil,
	"package": {
		"actions":      "read",
		"attestations": "write",
		"contents":     "read",
		"id-token":     "write",
	},
	"release": {
		"actions":  "read",
		"contents": "write",
	},
}

var reviewedJobStepNames = map[string][]string{
	"check": {
		"Check out tagged commit",
		"Set up Go",
		"Set up mise tools",
		"Run checks",
	},
	"full-chain-e2e": {
		"Check out tagged commit",
		"Set up Go",
		"Run hermetic full-chain journeys",
		"Upload failure artifacts",
	},
	"validate": {
		"Check out tagged commit",
		"Validate strict SemVer tag",
		"Verify tag provenance",
		"Refuse existing release",
	},
	"build": {
		"Check out tagged commit",
		"Set up Go",
		"Build native binary",
		"Smoke built binary",
		"Upload native binary",
	},
	"package": {
		"Check out tagged commit",
		"Download native binary",
		"Build deterministic archive",
		"Generate target SPDX SBOM",
		"Attest target build provenance",
		"Attach target provenance bundle",
		"Attest target SBOM",
		"Attach target SBOM bundle",
		"Upload target release assets",
	},
	"release": {
		"Check out tagged commit",
		"Download target release assets",
		"Generate release checksums",
		"Classify release tag",
		"Create immutable release",
	},
}

var releaseBuildMatrix = []map[string]string{
	{"target": "linux-amd64", "runner": "ubuntu-24.04", "goos": "linux", "goarch": "amd64"},
	{"target": "linux-arm64", "runner": "ubuntu-24.04-arm", "goos": "linux", "goarch": "arm64"},
	{"target": "darwin-amd64", "runner": "macos-15-intel", "goos": "darwin", "goarch": "amd64"},
	{"target": "darwin-arm64", "runner": "macos-15", "goos": "darwin", "goarch": "arm64"},
}

type workflow struct {
	// Typing On as a nested string-slice map rejects bare null triggers
	// (such as workflow_call:), scalar triggers, and extra event filters.
	On          map[string]map[string][]string `yaml:"on"`
	Permissions map[string]string              `yaml:"permissions"`
	Jobs        map[string]workflowJob         `yaml:"jobs"`
}

type workflowJob struct {
	Needs       stringList        `yaml:"needs"`
	RunsOn      string            `yaml:"runs-on"`
	If          string            `yaml:"if"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Strategy    workflowStrategy  `yaml:"strategy"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowStrategy struct {
	Matrix workflowMatrix `yaml:"matrix"`
}

type workflowMatrix struct {
	Include []map[string]string `yaml:"include"`
}

type workflowStep struct {
	Name           string            `yaml:"name"`
	ID             string            `yaml:"id"`
	If             string            `yaml:"if"`
	Uses           string            `yaml:"uses"`
	Run            string            `yaml:"run"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	With           map[string]string `yaml:"with"`
	Env            map[string]string `yaml:"env"`
}

type stringList []string

func (values *stringList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*values = []string{node.Value}
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("needs must be a string or sequence")
	}
	return node.Decode((*[]string)(values))
}

func TestWorkflowDirectoryContainsOnlyReleaseWorkflow(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{releaseWorkflowFile}) {
		t.Fatalf(".github/workflows entries = %v, want [%s]", names, releaseWorkflowFile)
	}

	wf := loadReleaseWorkflow(t)
	wantOn := map[string]map[string][]string{
		"push": {"tags": {releaseTagGlob}},
	}
	if !reflect.DeepEqual(wf.On, wantOn) {
		t.Fatalf("release workflow triggers = %#v, want %#v", wf.On, wantOn)
	}
}

func TestReleaseWorkflowPermissionsAndConditions(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	if !reflect.DeepEqual(wf.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("top-level permissions = %#v, want read-only contents", wf.Permissions)
	}
	if len(wf.Jobs) != len(reviewedJobPermissions) {
		t.Fatalf("workflow defines %d jobs, want %d reviewed jobs", len(wf.Jobs), len(reviewedJobPermissions))
	}
	for jobName, wantPerms := range reviewedJobPermissions {
		job, ok := wf.Jobs[jobName]
		if !ok {
			t.Fatalf("reviewed job %q missing from workflow", jobName)
		}
		if !reflect.DeepEqual(job.Permissions, wantPerms) {
			t.Errorf("%s job permissions = %#v, want %#v", jobName, job.Permissions, wantPerms)
		}
		if job.If != "" {
			t.Errorf("%s job must not override default gate condition: if=%q", jobName, job.If)
		}
		assertStepConditions(t, jobName, job)
	}
}

func assertStepConditions(t *testing.T, jobName string, job workflowJob) {
	t.Helper()
	for _, step := range job.Steps {
		wantIf := ""
		if jobName == "full-chain-e2e" && step.Name == "Upload failure artifacts" {
			wantIf = "failure()"
		}
		if step.If != wantIf {
			t.Errorf("%s/%s if = %q, want %q", jobName, step.Name, step.If, wantIf)
		}
	}
}

func TestReleaseWorkflowDependencyGraph(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	if len(wf.Jobs) != len(reviewedJobNeeds) {
		t.Fatalf("workflow defines %d jobs, want %d reviewed jobs", len(wf.Jobs), len(reviewedJobNeeds))
	}
	for jobName, wantNeeds := range reviewedJobNeeds {
		job, ok := wf.Jobs[jobName]
		if !ok {
			t.Fatalf("reviewed job %q missing from workflow", jobName)
		}
		gotNeeds := append([]string(nil), job.Needs...)
		sort.Strings(gotNeeds)
		if !reflect.DeepEqual(gotNeeds, wantNeeds) {
			t.Fatalf("%s job needs = %v, want %v", jobName, gotNeeds, wantNeeds)
		}
	}

	build := wf.Jobs["build"]
	if !reflect.DeepEqual(matrixTargets(build), releaseTargets) {
		t.Fatalf("build targets = %v, want %v", matrixTargets(build), releaseTargets)
	}
	if !reflect.DeepEqual(build.Strategy.Matrix.Include, releaseBuildMatrix) {
		t.Fatalf("build matrix = %#v, want %#v", build.Strategy.Matrix.Include, releaseBuildMatrix)
	}
	if build.RunsOn != "${{ matrix.runner }}" {
		t.Fatalf("build runner selector = %q", build.RunsOn)
	}
	buildStep := findNamedStep(t, build, "Build native binary")
	// GOFLAGS is pinned empty because inherited runner environment otherwise
	// injects arbitrary build options (e.g. -toolexec) into the release build.
	wantBuildEnv := map[string]string{
		"CGO_ENABLED": "0",
		"GOFLAGS":     "",
		"GOOS":        "${{ matrix.goos }}",
		"GOARCH":      "${{ matrix.goarch }}",
	}
	if !reflect.DeepEqual(buildStep.Env, wantBuildEnv) {
		t.Fatalf("native build env = %#v, want %#v", buildStep.Env, wantBuildEnv)
	}
	if buildStep.Run != reviewedReleaseBuildRun {
		t.Fatalf("native build script mismatch:\n--- got ---\n%s\n--- want ---\n%s", buildStep.Run, reviewedReleaseBuildRun)
	}
	smokeStep := findNamedStep(t, build, "Smoke built binary")
	if !strings.Contains(smokeStep.Run, "./serverpro --version") {
		t.Fatalf("smoke step does not verify built binary version: %q", smokeStep.Run)
	}
	if !reflect.DeepEqual(matrixTargets(wf.Jobs["package"]), releaseTargets) {
		t.Fatalf("package targets = %v, want %v", matrixTargets(wf.Jobs["package"]), releaseTargets)
	}
}

func TestReleaseWorkflowActionsArePinnedAndUncached(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	goVersion := goModVersion(t)
	counts := map[string]int{}
	for jobName, job := range wf.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" {
				continue
			}
			repo := assertPinnedActionStep(t, jobName, step, goVersion)
			counts[repo]++
		}
	}
	if !reflect.DeepEqual(counts, reviewedActionCounts) {
		t.Errorf("workflow action counts = %#v, want %#v", counts, reviewedActionCounts)
	}
}

func assertPinnedActionStep(t *testing.T, jobName string, step workflowStep, goVersion string) string {
	t.Helper()
	repo, _, hasPin := strings.Cut(step.Uses, "@")
	wantUses, reviewed := reviewedActionPins[repo]
	if !hasPin || !reviewed || step.Uses != wantUses {
		t.Fatalf("%s/%s uses %q, want reviewed pin %q", jobName, step.Name, step.Uses, wantUses)
	}
	switch repo {
	case "actions/checkout":
		want := map[string]string{"persist-credentials": "false"}
		if !reflect.DeepEqual(step.With, want) {
			t.Errorf("%s/%s checkout with = %#v, want %#v", jobName, step.Name, step.With, want)
		}
	case "actions/setup-go":
		// setup-go caches by default when cache is omitted; requiring exact
		// keys fails closed against re-enabling cache or check-latest drift.
		want := map[string]string{"go-version": goVersion, "cache": "false"}
		if !reflect.DeepEqual(step.With, want) {
			t.Errorf("%s/%s setup-go with = %#v, want %#v", jobName, step.Name, step.With, want)
		}
	case "jdx/mise-action":
		// mise-action caches by default and accepts inline mise_toml; exact
		// keys reject both cache poisoning and drift away from mise.toml.
		want := map[string]string{"version": releaseMiseVersion, "cache": "false"}
		if !reflect.DeepEqual(step.With, want) {
			t.Errorf("%s/%s mise-action with = %#v, want %#v", jobName, step.Name, step.With, want)
		}
		if step.TimeoutMinutes != releaseMiseTimeoutMin {
			t.Errorf("%s/%s timeout-minutes = %d, want %d", jobName, step.Name, step.TimeoutMinutes, releaseMiseTimeoutMin)
		}
	}
	return repo
}

func TestReleaseWorkflowCheckAndFullChainE2EJobs(t *testing.T) {
	wf := loadReleaseWorkflow(t)

	checkJob := wf.Jobs["check"]
	if checkJob.RunsOn != releaseUbuntuRunner {
		t.Fatalf("check runs-on = %q, want %q", checkJob.RunsOn, releaseUbuntuRunner)
	}
	wantCheckEnv := map[string]string{"UV_PYTHON": releaseUVPython}
	if !reflect.DeepEqual(checkJob.Env, wantCheckEnv) {
		t.Fatalf("check env = %#v, want %#v", checkJob.Env, wantCheckEnv)
	}
	if runChecks := findNamedStep(t, checkJob, "Run checks"); runChecks.Run != "make check" {
		t.Fatalf("check Run checks command = %q, want %q", runChecks.Run, "make check")
	}

	e2eJob := wf.Jobs["full-chain-e2e"]
	if e2eJob.RunsOn != releaseUbuntuRunner {
		t.Fatalf("full-chain-e2e runs-on = %q, want %q", e2eJob.RunsOn, releaseUbuntuRunner)
	}
	runE2E := findNamedStep(t, e2eJob, "Run hermetic full-chain journeys")
	if runE2E.Run != "make test-full-chain-e2e" {
		t.Fatalf("full-chain-e2e run command = %q, want %q", runE2E.Run, "make test-full-chain-e2e")
	}
	if runE2E.Env["SERVERPRO_E2E_ARTIFACT_DIR"] != fullChainArtifactDir {
		t.Fatalf("full-chain-e2e artifact dir env = %q, want %q", runE2E.Env["SERVERPRO_E2E_ARTIFACT_DIR"], fullChainArtifactDir)
	}
	upload := findNamedStep(t, e2eJob, "Upload failure artifacts")
	if upload.If != "failure()" {
		t.Fatalf("full-chain-e2e artifact upload condition = %q, want failure()", upload.If)
	}
	if upload.Uses != reviewedActionPins["actions/upload-artifact"] {
		t.Fatalf("full-chain-e2e upload action = %q, want %q", upload.Uses, reviewedActionPins["actions/upload-artifact"])
	}
	wantUploadWith := map[string]string{
		"name":              fullChainArtifactName,
		"path":              fullChainArtifactDir,
		"if-no-files-found": "warn",
		"retention-days":    fullChainRetentionDays,
	}
	if !reflect.DeepEqual(upload.With, wantUploadWith) {
		t.Fatalf("full-chain-e2e upload with = %#v, want %#v", upload.With, wantUploadWith)
	}
}

func TestReleaseWorkflowJobStepsAreCanonical(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	if len(wf.Jobs) != len(reviewedJobStepNames) {
		t.Fatalf("workflow defines %d jobs, want %d reviewed jobs", len(wf.Jobs), len(reviewedJobStepNames))
	}
	for jobName, want := range reviewedJobStepNames {
		t.Run(jobName, func(t *testing.T) {
			job := wf.Jobs[jobName]
			if !jobStepsAreCanonical(job, want) {
				t.Fatalf("%s job steps = %v, want %v", jobName, stepNames(job), want)
			}
		})
	}
}

func TestJobStepsAreCanonical(t *testing.T) {
	canonical := func(names ...string) workflowJob {
		job := workflowJob{}
		for _, name := range names {
			job.Steps = append(job.Steps, workflowStep{Name: name})
		}
		return job
	}
	want := reviewedJobStepNames["build"]
	for _, test := range []struct {
		name string
		job  workflowJob
		want bool
	}{
		{name: "exact", job: canonical(want...), want: true},
		{name: "inserted", job: canonical(append(append([]string{}, want[:4]...), "Overwrite binary", "Upload native binary")...), want: false},
		{name: "duplicate", job: canonical(append(append([]string{}, want...), "Build native binary")...), want: false},
		{name: "reordered", job: canonical("Set up Go", "Check out tagged commit", "Build native binary", "Smoke built binary", "Upload native binary"), want: false},
		{name: "missing", job: canonical(want[:4]...), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := jobStepsAreCanonical(test.job, want); got != test.want {
				t.Fatalf("canonical step check = %t, want %t", got, test.want)
			}
		})
	}
}

func TestReleaseWorkflowPairsEachArchiveWithItsSBOM(t *testing.T) {
	job := loadReleaseWorkflow(t).Jobs["package"]
	sbom := findStep(t, job, "anchore/sbom-action@")
	if !strings.Contains(sbom.With["output-file"], "${{ matrix.target }}") {
		t.Fatalf("SBOM output is not target-scoped: %q", sbom.With["output-file"])
	}
	attest := findStep(t, job, "actions/attest@")
	if !strings.Contains(attest.With["subject-path"], "${{ matrix.target }}") || !strings.Contains(attest.With["sbom-path"], "${{ matrix.target }}") {
		t.Fatalf("SBOM attestation is not target-paired: %+v", attest.With)
	}
}

func TestReleaseWorkflowPublishesCompleteTargetEvidence(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	job := wf.Jobs["package"]
	if !reflect.DeepEqual(matrixTargets(job), releaseTargets) {
		t.Fatalf("package targets = %v, want %v", matrixTargets(job), releaseTargets)
	}
	buildUpload := findNamedStep(t, wf.Jobs["build"], "Upload native binary")
	if buildUpload.With["name"] != "binary-${{ matrix.target }}" || buildUpload.With["path"] != "out/serverpro" || buildUpload.With["if-no-files-found"] != "error" {
		t.Errorf("native binary upload mismatch: %+v", buildUpload.With)
	}
	packageDownload := findNamedStep(t, job, "Download native binary")
	if packageDownload.With["name"] != "binary-${{ matrix.target }}" || packageDownload.With["path"] != "binary" {
		t.Errorf("native binary download mismatch: %+v", packageDownload.With)
	}
	archive := findNamedStep(t, job, "Build deterministic archive")
	for _, exact := range []string{
		`name="serverpro-${GITHUB_REF_NAME}-${{ matrix.target }}"`,
		`install -m 0755 binary/serverpro "dist/${name}/serverpro"`,
		`-czf "dist/${name}.tar.gz"`,
	} {
		if !strings.Contains(archive.Run, exact) {
			t.Errorf("archive command missing exact asset edge %q: %s", exact, archive.Run)
		}
	}

	const (
		archivePath          = "dist/serverpro-${{ github.ref_name }}-${{ matrix.target }}.tar.gz"
		spdxPath             = "dist/serverpro-${{ github.ref_name }}-${{ matrix.target }}.spdx.json"
		provenanceBundlePath = `dist/serverpro-${GITHUB_REF_NAME}-${{ matrix.target }}.provenance.sigstore.json`
		sbomBundlePath       = `dist/serverpro-${GITHUB_REF_NAME}-${{ matrix.target }}.sbom.sigstore.json`
	)
	provenance := findNamedStep(t, job, "Attest target build provenance")
	attachProvenance := findNamedStep(t, job, "Attach target provenance bundle")
	sbom := findNamedStep(t, job, "Generate target SPDX SBOM")
	attestSBOM := findNamedStep(t, job, "Attest target SBOM")
	attachSBOM := findNamedStep(t, job, "Attach target SBOM bundle")
	if provenance.ID != "provenance" || provenance.With["subject-path"] != archivePath {
		t.Errorf("provenance subject mismatch: %+v", provenance)
	}
	if attachProvenance.Env["BUNDLE_PATH"] != "${{ steps.provenance.outputs.bundle-path }}" || !strings.Contains(attachProvenance.Run, `install -m 0644 "${BUNDLE_PATH}" "`+provenanceBundlePath+`"`) {
		t.Errorf("provenance bundle destination mismatch: %+v", attachProvenance)
	}
	if sbom.With["file"] != "binary/serverpro" || sbom.With["format"] != "spdx-json" || sbom.With["output-file"] != spdxPath {
		t.Errorf("SPDX production mismatch: %+v", sbom.With)
	}
	if attestSBOM.ID != "sbom-attestation" || attestSBOM.With["subject-path"] != archivePath || attestSBOM.With["sbom-path"] != spdxPath {
		t.Errorf("SBOM attestation mismatch: %+v", attestSBOM)
	}
	if attachSBOM.Env["BUNDLE_PATH"] != "${{ steps.sbom-attestation.outputs.bundle-path }}" || !strings.Contains(attachSBOM.Run, `install -m 0644 "${BUNDLE_PATH}" "`+sbomBundlePath+`"`) {
		t.Errorf("SBOM bundle destination mismatch: %+v", attachSBOM)
	}

	upload := findNamedStep(t, job, "Upload target release assets")
	if upload.With["name"] != "release-${{ matrix.target }}" || upload.With["path"] != "dist/*" || upload.With["if-no-files-found"] != "error" {
		t.Errorf("target evidence upload mismatch: %+v", upload.With)
	}
	releaseJob := wf.Jobs["release"]
	download := findNamedStep(t, releaseJob, "Download target release assets")
	checksums := findNamedStep(t, releaseJob, "Generate release checksums")
	publish := findNamedStep(t, releaseJob, "Create immutable release")
	if download.With["pattern"] != "release-*" || download.With["path"] != "dist" || download.With["merge-multiple"] != "true" {
		t.Errorf("release asset collection mismatch: %+v", download.With)
	}
	if !strings.Contains(checksums.Run, "LC_ALL=C shasum -a 256 -- *.tar.gz *.json > SHA256SUMS") {
		t.Errorf("checksum asset set mismatch: %s", checksums.Run)
	}
	if !strings.Contains(publish.Run, `gh release create "${GITHUB_REF_NAME}" ./dist/*`) {
		t.Errorf("publication asset set mismatch: %s", publish.Run)
	}
}

func TestReleaseWorkflowNeverClobbersRelease(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	for jobName, job := range wf.Jobs {
		for _, step := range job.Steps {
			for _, forbidden := range []string{"gh release edit", "--clobber"} {
				if strings.Contains(step.Run, forbidden) {
					t.Errorf("%s/%s contains forbidden release mutation %q", jobName, step.Name, forbidden)
				}
			}
		}
	}
}

func TestReleaseWorkflowClassifiesPrereleasesBeforePublication(t *testing.T) {
	job := loadReleaseWorkflow(t).Jobs["release"]
	var classify, publish workflowStep
	classifyCount, publishCount := 0, 0
	classifyIndex, publishIndex := -1, -1
	for index, step := range job.Steps {
		if step.ID == "classify-release" {
			classify = step
			classifyCount++
			classifyIndex = index
		}
		if strings.Contains(step.Run, "gh release create") {
			publish = step
			publishCount++
			publishIndex = index
		}
	}
	if classifyCount != 1 || publishCount != 1 || classifyIndex >= publishIndex {
		t.Fatalf("classification count/index=%d/%d publication count/index=%d/%d", classifyCount, classifyIndex, publishCount, publishIndex)
	}
	if !strings.Contains(classify.Run, "classify-release-tag.sh") {
		t.Fatalf("release classification step missing: %+v", classify)
	}
	if publish.Env["IS_PRERELEASE"] != "${{ steps.classify-release.outputs.prerelease }}" || !strings.Contains(publish.Run, "--prerelease") {
		t.Fatalf("publication does not consume prerelease classification: %+v", publish)
	}
}

func TestReleaseWorkflowValidatesTagProvenanceBeforeBuild(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	validateJob := wf.Jobs["validate"]
	validateTag := findNamedStep(t, validateJob, "Validate strict SemVer tag")
	if validateTag.Run != `bash scripts/validate-release-tag.sh "${GITHUB_REF_NAME}"` {
		t.Fatalf("tag validation invocation = %q", validateTag.Run)
	}
	step := findNamedStep(t, validateJob, "Verify tag provenance")
	if step.Run != `bash scripts/validate-release-provenance.sh "${GITHUB_REF_NAME}"` {
		t.Fatalf("provenance validation invocation = %q", step.Run)
	}
	if step.Env["GH_TOKEN"] != "${{ github.token }}" || step.Env["RELEASE_SIGNER_ALLOWLIST"] != "${{ vars.RELEASE_SIGNER_ALLOWLIST }}" {
		t.Fatalf("provenance validation env = %#v", step.Env)
	}
	refuseExisting := findNamedStep(t, validateJob, "Refuse existing release")
	if refuseExisting.Env["GH_TOKEN"] != "${{ github.token }}" {
		t.Fatalf("existing release guard missing GH_TOKEN: %#v", refuseExisting.Env)
	}
	if !strings.Contains(refuseExisting.Run, `gh release view "${GITHUB_REF_NAME}"`) {
		t.Fatalf("existing release guard missing gh release view: %q", refuseExisting.Run)
	}
	for index, item := range validateJob.Steps {
		if item.Name == "Verify tag provenance" && index == 0 {
			t.Fatal("provenance validation runs before the tagged commit is checked out")
		}
	}
}

// goModVersion reads the Go version directive from go.mod so release.yml stays
// bound to the module toolchain pin rather than drifting to a separate copy.
func goModVersion(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	t.Fatal("go.mod missing go version directive")
	return ""
}

func loadReleaseWorkflow(t *testing.T) workflow {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", releaseWorkflowFile)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wf workflow
	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}
	return wf
}

func jobStepsAreCanonical(job workflowJob, want []string) bool {
	return reflect.DeepEqual(stepNames(job), want)
}

func stepNames(job workflowJob) []string {
	names := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		names = append(names, step.Name)
	}
	return names
}

func matrixTargets(job workflowJob) []string {
	targets := make([]string, 0, len(job.Strategy.Matrix.Include))
	for _, item := range job.Strategy.Matrix.Include {
		target := item["target"]
		if target == "" && item["goos"] != "" && item["goarch"] != "" {
			target = item["goos"] + "-" + item["goarch"]
		}
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func findNamedStep(t *testing.T, job workflowJob, name string) workflowStep {
	t.Helper()
	for _, step := range job.Steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("step named %q missing", name)
	return workflowStep{}
}

func findStep(t *testing.T, job workflowJob, actionPrefix string) workflowStep {
	t.Helper()
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, actionPrefix) {
			return step
		}
	}
	t.Fatalf("step using %q missing", actionPrefix)
	return workflowStep{}
}
