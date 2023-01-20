//go:build mage
// +build mage

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dagger.io/dagger"
	"github.com/containers/image/v5/docker/reference"
	"github.com/magefile/mage/mg"
)

const (
	// https://hub.docker.com/_/golang/tags?page=1&name=alpine
	alpineVersion = "3.17"
	goVersion     = "1.19"

	// https://github.com/golangci/golangci-lint/releases
	golangciLintVersion = "1.50.1"

	// https://hub.docker.com/_/docker/tags?page=1&name=cli
	dockerVersion = "20.10"

	// https://hub.docker.com/r/flyio/flyctl/tags
	flyctlVersion = "0.0.450"

	binaryName = "registry-redirect"
	_imageName = "registry.fly.io/dagger-registry"
)

// golangci-lint
func Lint(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()
	lint(ctx, d)
}

func lint(ctx context.Context, d *dagger.Client) {
	exitCode, err := d.Container().
		From(fmt.Sprintf("golangci/golangci-lint:v%s-alpine", golangciLintVersion)).
		WithMountedCache("/go/pkg/mod", d.CacheVolume("gomod")).
		WithMountedDirectory("/src", sourceCode(d)).WithWorkdir("/src").
		WithExec([]string{"golangci-lint", "run", "--color", "always"}).
		ExitCode(ctx)

	if err != nil {
		unavailableErr(err)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// go test
func Test(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()
	test(ctx, d)
}

func test(ctx context.Context, d *dagger.Client) {
	exitCode, err := d.Container().
		From(fmt.Sprintf("golang:%s-alpine%s", goVersion, alpineVersion)).
		WithMountedDirectory("/src", sourceCode(d)).WithWorkdir("/src").
		WithMountedCache("/go/pkg/mod", d.CacheVolume("gomod")).
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{"go", "test", "./..."}).
		ExitCode(ctx)

	if err != nil {
		unavailableErr(err)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// binary artefact used in container image
func Build(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()
	_ = build(ctx, d)
}

func build(ctx context.Context, d *dagger.Client) *dagger.File {
	binaryPath := fmt.Sprintf("build/%s", binaryName)

	buildBinary := d.Container().
		From(fmt.Sprintf("golang:%s-alpine%s", goVersion, alpineVersion)).
		WithMountedDirectory("/src", sourceCode(d)).WithWorkdir("/src").
		WithMountedCache("/go/pkg/mod", d.CacheVolume("gomod")).
		WithExec([]string{"go", "build", "-o", binaryPath})

	_, err := buildBinary.ExitCode(ctx)
	if err != nil {
		createErr(err)
	}

	return buildBinary.File(binaryPath)
}

func sourceCode(d *dagger.Client) *dagger.Directory {
	return d.Host().Directory(".", dagger.HostDirectoryOpts{
		Include: []string{
			"LICENSE",
			"README.md",
			"go.mod",
			"go.sum",
			"**/*.go",
		},
	})
}

// Docker client with private registry
func Auth(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()

	flyctl := flyctlWithDockerConfig(ctx, d)
	authDocker(ctx, d, flyctl)
}

func authDocker(ctx context.Context, d *dagger.Client, c *dagger.Container) {
	hostDockerConfigDir := os.Getenv("DOCKER_CONFIG")
	if hostDockerConfigDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			readErr(err)
		}
		hostDockerConfigDir = filepath.Join(home, ".docker")
	}
	hostDockerClientConfig := filepath.Join(hostDockerConfigDir, "config.json")

	_, err := c.File(".docker/config.json").Export(ctx, hostDockerClientConfig)
	if err != nil {
		createErr(err)
	}
}

// container image to private registry
func Publish(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()

	imageReference := publish(ctx, d)
	fmt.Println("\n🐳", imageReference)
}

func publish(ctx context.Context, d *dagger.Client) string {
	binary := build(ctx, d)
	return publishImage(ctx, d, binary)
}

// zero-downtime deploy container image
func Deploy(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()

	deploy(ctx, d, os.Getenv("IMAGE_REFERENCE"))
}

func deploy(ctx context.Context, d *dagger.Client, imageReference string) {
	imageReferenceFlyValid, err := reference.ParseDockerRef(imageReference)
	if err != nil {
		misconfigureErr(err)
	}

	flyctl := flyctlWithDockerConfig(ctx, d)
	flyctl = flyctl.WithExec([]string{"deploy", "--image", imageReferenceFlyValid.String()})

	exitCode, err := flyctl.ExitCode(ctx)
	if err != nil {
		unavailableErr(err)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// [lints, tests, auths], builds, publishes & deploys a new version of the app
func All(ctx context.Context) {
	// TODO: re-use the same client, run in parallel with err.Go
	mg.CtxDeps(ctx, Lint, Test, Auth)

	d := daggerClient(ctx)
	defer d.Close()

	imageReference := publish(ctx, d)
	deploy(ctx, d, imageReference)
}

// stream app logs
func Logs(ctx context.Context) {
	d := daggerClient(ctx)
	defer d.Close()

	// This command does not return,
	// therefore it will never be cached,
	// and it can be run multiple times
	_, err := flyctlWithDockerConfig(ctx, d).
		WithExec([]string{"logs"}).
		Stdout(ctx)

	if err != nil {
		unavailableErr(err)
	}
}

func daggerClient(ctx context.Context) *dagger.Client {
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		unavailableErr(err)
	}
	return client
}

func publishImage(ctx context.Context, d *dagger.Client, binary *dagger.File) string {
	ref := fmt.Sprintf("%s:%s", imageName(), imageTag())

	refWithSHA, err := d.Container().
		From(fmt.Sprintf("alpine:%s", alpineVersion)).
		WithFile(fmt.Sprintf("/%s", binaryName), binary).
		WithEntrypoint([]string{fmt.Sprintf("/%s", binaryName)}).
		Publish(ctx, ref)

	if err != nil {
		unavailableErr(err)
	}

	return refWithSHA
}

func flyctlWithDockerConfig(ctx context.Context, d *dagger.Client) *dagger.Container {
	flyToken := hostEnv(ctx, d.Host(), "FLY_API_TOKEN").Secret()

	flyctl := d.Container().
		From(fmt.Sprintf("flyio/flyctl:v%s", flyctlVersion)).
		WithSecretVariable("FLY_API_TOKEN", flyToken).
		WithMountedFile("fly.toml", flyConfig(d)).
		WithExec([]string{"auth", "docker"})

	exitCode, err := flyctl.ExitCode(ctx)

	if err != nil {
		createErr(err)
	}

	if exitCode != 0 {
		createErr(errors.New("Failed to add registry.fly.io as a Docker authenticated registry"))
	}

	return flyctl
}

func flyConfig(d *dagger.Client) *dagger.File {
	return d.Host().Directory(".").File("fly.toml")
}

func imageName() string {
	envImageURL := os.Getenv("IMAGE_URL")
	if envImageURL != "" {
		return envImageURL
	}

	return _imageName
}

func imageTag() string {
	envImageTag := os.Getenv("GITHUB_SHA")
	if envImageTag != "" {
		return envImageTag
	}

	now := time.Now().UTC().Format(time.RFC3339)
	validNow := strings.ReplaceAll(now, ":", ".")
	return validNow
}

func hostEnv(ctx context.Context, host *dagger.Host, varName string) *dagger.HostVariable {
	hostEnv := host.EnvVariable(varName)
	hostEnvVal, err := hostEnv.Value(ctx)
	if err != nil {
		unavailableErr(err)
	}
	if hostEnvVal == "" {
		misconfigureErr(errors.New(fmt.Sprintf("💥 env var %s must be set\n", varName)))
	}
	return hostEnv
}

// https://man.openbsd.org/sysexits
func readErr(err error) {
	fmt.Fprintf(os.Stderr, err.Error())
	os.Exit(66)
}
func unavailableErr(err error) {
	fmt.Fprintf(os.Stderr, err.Error())
	os.Exit(69)
}
func createErr(err error) {
	fmt.Fprintf(os.Stderr, err.Error())
	os.Exit(73)
}
func misconfigureErr(err error) {
	fmt.Fprintf(os.Stderr, err.Error())
	os.Exit(78)
}
