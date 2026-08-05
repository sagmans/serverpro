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

const reviewedReleaseLdflags = `-ldflags "-s -w -buildid= -X github.com/sagmans/serverpro/internal/cli.Version=${GITHUB_REF_NAME}"`

// reviewedReleaseBuildRun is the canonical build script. Comparing the whole
// step text fail-closed is deliberate: parsing shell/Go flags proved
// unbounded, so any build change must update this reviewed string.
const reviewedReleaseBuildRun = "set -eu\n" +
	"mkdir -p out\n" +
	"go build \\\n" +
	"  -trimpath \\\n" +
	"  -buildvcs=false \\\n" +
	"  " + reviewedReleaseLdflags + " \\\n" +
	"  -o out/serverpro \\\n" +
	"  ./cmd/serverpro\n"

var releaseTargets = []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"}

var reviewedJobStepNames = map[string][]string{
	"validate": {
		"Check out tagged commit",
		"Validate strict SemVer tag",
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
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs    stringList       `yaml:"needs"`
	RunsOn   string           `yaml:"runs-on"`
	Strategy workflowStrategy `yaml:"strategy"`
	Steps    []workflowStep   `yaml:"steps"`
}

type workflowStrategy struct {
	Matrix workflowMatrix `yaml:"matrix"`
}

type workflowMatrix struct {
	Include []map[string]string `yaml:"include"`
}

type workflowStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
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

func TestReleaseWorkflowDependencyGraph(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	for _, dependency := range []string{"checks", "validate", "build", "package"} {
		if !jobDependsOn(wf.Jobs, "release", dependency, map[string]bool{}) {
			t.Fatalf("release job does not depend transitively on %q", dependency)
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
	if !reflect.DeepEqual(matrixTargets(wf.Jobs["package"]), releaseTargets) {
		t.Fatalf("package targets = %v, want %v", matrixTargets(wf.Jobs["package"]), releaseTargets)
	}
}

func TestReleaseWorkflowJobStepsAreCanonical(t *testing.T) {
	wf := loadReleaseWorkflow(t)
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

func loadReleaseWorkflow(t *testing.T) workflow {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
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

func jobDependsOn(jobs map[string]workflowJob, current, target string, seen map[string]bool) bool {
	if seen[current] {
		return false
	}
	seen[current] = true
	for _, dependency := range jobs[current].Needs {
		if dependency == target || jobDependsOn(jobs, dependency, target, seen) {
			return true
		}
	}
	return false
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
