package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/distribution/digest"
	"github.com/docker/docker/pkg/integration/checker"
	"github.com/go-check/check"
)

func (s *DockerSuite) TestSavePartialAndLoad(c *check.C) {

	regTags := regexp.MustCompile(`(?s)"RepoTags": \[[^][]*\]`)
	regParent := regexp.MustCompile(`(?s)"Parent": "[a-z0-9:]*"`)
	inspectImage := func(img string) string {
		inspectCmd := exec.Command(dockerBinary, "inspect", img)
		result, _, _, err := runCommandWithStdoutStderr(inspectCmd)
		if err != nil {
			c.Fatalf("the image should exist: %v %v", img, err)
		}
		result = regTags.ReplaceAllLiteralString(result, `"RepoTags": []`)
		result = regParent.ReplaceAllLiteralString(result, `"Parent": ""`)
		return result
	}
	makeImage := func(from string, tag string) string {
		buildCmd := exec.Command("sh", "-c", fmt.Sprintf(
				"(echo 'FROM %s' ; echo 'RUN echo %q >> /tags') | %v build -q -t %v -",
				from, tag, dockerBinary, tag))

		imageID := ""
		if out, stderr, _, err := runCommandWithStdoutStderr(buildCmd); err != nil {
			c.Fatalf("failed to build an image: %v %v %v", out, stderr, err)
		} else {
			imageID = strings.TrimSpace(out)
		}
		return imageID
	}
	regLayerID := regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
	splitLines := func(txt string) []string {
		return strings.Split(strings.Trim(txt, "\n"), "\n")
	}
	listImageLayers := func(img string) []string {
		cmd := exec.Command("sh", "-c",
			fmt.Sprintf("%v save --exclude=all %s | %v load --print-excludes",
					dockerBinary, img, dockerBinary))
		out, stderr, _, err := runCommandWithStdoutStderr(cmd)
		if err != nil {
			c.Fatalf("failed to list layer ids: %v %v %v", out, stderr, err)
		}

		layers := splitLines(out)
		for _, l := range layers {
			if !regLayerID.MatchString(l) {
				c.Fatalf("'--print-excludes' returned invalid layer id %q", l)
			}
		}
		return layers
	}

	saveImage := func(tag string, filename string, exclude []string) {
		exArgs := make([]string, len(exclude))
		for i, ex := range exclude {
			exArgs[i] = fmt.Sprintf("-e %s", ex)
		}

		saveCmdTemplate := `%v save -o %s %s %v`
		saveCmdFinal := fmt.Sprintf(saveCmdTemplate, dockerBinary, filename,
			strings.Join(exArgs, " "), tag)
		saveCmd := exec.Command("bash", "-c", saveCmdFinal)
		if out, _, err := runCommandWithOutput(saveCmd); err != nil {
			c.Fatalf("failed to save image: %v %v", out, err)
		}
	}
	tagImage := func(img string, tag string) {
		tagCmdTemplate := `%v tag %s %s`
		tagCmdFinal := fmt.Sprintf(tagCmdTemplate, dockerBinary, img, tag)
		tagCmd := exec.Command("bash", "-c", tagCmdFinal)
		if out, _, err := runCommandWithOutput(tagCmd); err != nil {
			c.Fatalf("failed to tag image: %v %v", out, err)
		}
	}
	loadImage := func(filename string, must_succeed bool) {
		loadCmdTemplate := `%s load -i %s`
		loadCmdFinal := fmt.Sprintf(loadCmdTemplate, dockerBinary, filename)
		loadCmd := exec.Command("bash", "-c", loadCmdFinal)
		out, _, err := runCommandWithOutput(loadCmd)
		if must_succeed && (err != nil) {
			c.Fatalf("failed to load image: %v %v", out, err)
		} else if !must_succeed && (err == nil) {
			c.Fatalf("image load must fail")
		}
	}
	loadImageTestExcludes := func(filename string, expect_excludes []string) {
		cmd := exec.Command(dockerBinary, "load", "--print-excludes", "-i", filename)
		out, stderr, _, err := runCommandWithStdoutStderr(cmd)
		if err != nil {
			c.Fatalf("failed to load image: %v %v %v", out, stderr, err)
		}
		excludes := splitLines(out)
		ref      := expect_excludes
		sort.Strings(excludes)
		sort.Strings(ref)
		if !reflect.DeepEqual(excludes, ref) {
			c.Fatalf("load image produced invalid exclude id:\n\texpected: %v\n\tgot:      %v",
				ref, excludes)
		}
	}
	testImage := func(img string, inspectBefore string) {
		inspectAfter := inspectImage(img)
		if inspectBefore != inspectAfter {
			c.Fatalf("inspect is not the same after a save / load")
		}
	}

	repoName := "foobar-save-partial-load-test"
	tagFoo  := repoName + ":foo"
	tagBar  := repoName + ":bar"
	tagBusy := "busybox:latest"

	fileFooBar := "/tmp/" + repoName + "-foo-bar.tar"
	fileBar := "/tmp/" + repoName + "-bar.tar"

	idFoo := makeImage(tagBusy, tagFoo)
	idBar := makeImage(tagFoo,  tagBar)

	inspectFoo := inspectImage(tagFoo)
	inspectBar := inspectImage(tagBar)

	layersBusy := listImageLayers(tagBusy)
	layersFoo  := listImageLayers(tagFoo)
	layersBar  := listImageLayers(tagBar)

	saveImage((tagFoo + " " + tagBar), fileFooBar, layersBusy)
	saveImage(tagBar, fileBar,    layersFoo)

	// load foo + bar
	deleteImages(tagBar)
	// FIXME layers seem to be lazily removed
	//loadImageTestExcludes(fileFooBar, layersFoo)
	deleteImages(tagFoo)
	// FIXME layers seem to be lazily removed
	//loadImageTestExcludes(fileFooBar, layersBusy)

	loadImage(fileFooBar, true)
	testImage(idFoo, inspectFoo)
	testImage(idBar, inspectBar)

	loadImageTestExcludes(fileFooBar, layersBar)

	// load bar only
	tagImage(idFoo, tagFoo)
	deleteImages(idBar)
	// FIXME layers seem to be lazily removed
	//loadImageTestExcludes(fileFooBar, layersFoo)
	loadImage(fileBar, true)
	testImage(idBar, inspectBar)
	loadImageTestExcludes(fileFooBar, layersBar)

	// load bar but with foo missing
	deleteImages(tagFoo)
	deleteImages(tagBar)
	// FIXME layers seem to be lazily removed
	//loadImageTestExcludes(fileFooBar, layersBusy)
	//loadImage(fileBar, false)

	// cleanup
	os.Remove(fileFooBar)
	os.Remove(fileBar)
}

// save a repo using gz compression and try to load it using stdout
func (s *DockerSuite) TestSaveXzAndLoadRepoStdout(c *check.C) {
	testRequires(c, DaemonIsLinux)
	name := "test-save-xz-and-load-repo-stdout"
	dockerCmd(c, "run", "--name", name, "busybox", "true")

	repoName := "foobar-save-load-test-xz-gz"
	out, _ := dockerCmd(c, "commit", name, repoName)

	dockerCmd(c, "inspect", repoName)

	repoTarball, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", repoName),
		exec.Command("xz", "-c"),
		exec.Command("gzip", "-c"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save repo: %v %v", out, err))
	deleteImages(repoName)

	loadCmd := exec.Command(dockerBinary, "load")
	loadCmd.Stdin = strings.NewReader(repoTarball)
	out, _, err = runCommandWithOutput(loadCmd)
	c.Assert(err, checker.NotNil, check.Commentf("expected error, but succeeded with no error and output: %v", out))

	after, _, err := dockerCmdWithError("inspect", repoName)
	c.Assert(err, checker.NotNil, check.Commentf("the repo should not exist: %v", after))
}

// save a repo using xz+gz compression and try to load it using stdout
func (s *DockerSuite) TestSaveXzGzAndLoadRepoStdout(c *check.C) {
	testRequires(c, DaemonIsLinux)
	name := "test-save-xz-gz-and-load-repo-stdout"
	dockerCmd(c, "run", "--name", name, "busybox", "true")

	repoName := "foobar-save-load-test-xz-gz"
	dockerCmd(c, "commit", name, repoName)

	dockerCmd(c, "inspect", repoName)

	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", repoName),
		exec.Command("xz", "-c"),
		exec.Command("gzip", "-c"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save repo: %v %v", out, err))

	deleteImages(repoName)

	loadCmd := exec.Command(dockerBinary, "load")
	loadCmd.Stdin = strings.NewReader(out)
	out, _, err = runCommandWithOutput(loadCmd)
	c.Assert(err, checker.NotNil, check.Commentf("expected error, but succeeded with no error and output: %v", out))

	after, _, err := dockerCmdWithError("inspect", repoName)
	c.Assert(err, checker.NotNil, check.Commentf("the repo should not exist: %v", after))
}

func (s *DockerSuite) TestSaveSingleTag(c *check.C) {
	testRequires(c, DaemonIsLinux)
	repoName := "foobar-save-single-tag-test"
	dockerCmd(c, "tag", "busybox:latest", fmt.Sprintf("%v:latest", repoName))

	out, _ := dockerCmd(c, "images", "-q", "--no-trunc", repoName)
	cleanedImageID := strings.TrimSpace(out)

	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", fmt.Sprintf("%v:latest", repoName)),
		exec.Command("tar", "t"),
		exec.Command("grep", "-E", fmt.Sprintf("(^repositories$|%v)", cleanedImageID)))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save repo with image ID and 'repositories' file: %s, %v", out, err))
}

func (s *DockerSuite) TestSaveCheckTimes(c *check.C) {
	testRequires(c, DaemonIsLinux)
	repoName := "busybox:latest"
	out, _ := dockerCmd(c, "inspect", repoName)
	data := []struct {
		ID      string
		Created time.Time
	}{}
	err := json.Unmarshal([]byte(out), &data)
	c.Assert(err, checker.IsNil, check.Commentf("failed to marshal from %q: err %v", repoName, err))
	c.Assert(len(data), checker.Not(checker.Equals), 0, check.Commentf("failed to marshal the data from %q", repoName))
	tarTvTimeFormat := "2006-01-02 15:04"
	out, _, err = runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", repoName),
		exec.Command("tar", "tv"),
		exec.Command("grep", "-E", fmt.Sprintf("%s %s", data[0].Created.Format(tarTvTimeFormat), digest.Digest(data[0].ID).Hex())))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save repo with image ID and 'repositories' file: %s, %v", out, err))
}

func (s *DockerSuite) TestSaveImageId(c *check.C) {
	testRequires(c, DaemonIsLinux)
	repoName := "foobar-save-image-id-test"
	dockerCmd(c, "tag", "emptyfs:latest", fmt.Sprintf("%v:latest", repoName))

	out, _ := dockerCmd(c, "images", "-q", "--no-trunc", repoName)
	cleanedLongImageID := strings.TrimPrefix(strings.TrimSpace(out), "sha256:")

	out, _ = dockerCmd(c, "images", "-q", repoName)
	cleanedShortImageID := strings.TrimSpace(out)

	// Make sure IDs are not empty
	c.Assert(cleanedLongImageID, checker.Not(check.Equals), "", check.Commentf("Id should not be empty."))
	c.Assert(cleanedShortImageID, checker.Not(check.Equals), "", check.Commentf("Id should not be empty."))

	saveCmd := exec.Command(dockerBinary, "save", cleanedShortImageID)
	tarCmd := exec.Command("tar", "t")

	var err error
	tarCmd.Stdin, err = saveCmd.StdoutPipe()
	c.Assert(err, checker.IsNil, check.Commentf("cannot set stdout pipe for tar: %v", err))
	grepCmd := exec.Command("grep", cleanedLongImageID)
	grepCmd.Stdin, err = tarCmd.StdoutPipe()
	c.Assert(err, checker.IsNil, check.Commentf("cannot set stdout pipe for grep: %v", err))

	c.Assert(tarCmd.Start(), checker.IsNil, check.Commentf("tar failed with error: %v", err))
	c.Assert(saveCmd.Start(), checker.IsNil, check.Commentf("docker save failed with error: %v", err))
	defer func() {
		saveCmd.Wait()
		tarCmd.Wait()
		dockerCmd(c, "rmi", repoName)
	}()

	out, _, err = runCommandWithOutput(grepCmd)

	c.Assert(err, checker.IsNil, check.Commentf("failed to save repo with image ID: %s, %v", out, err))
}

// save a repo and try to load it using flags
func (s *DockerSuite) TestSaveAndLoadRepoFlags(c *check.C) {
	testRequires(c, DaemonIsLinux)
	name := "test-save-and-load-repo-flags"
	dockerCmd(c, "run", "--name", name, "busybox", "true")

	repoName := "foobar-save-load-test"

	deleteImages(repoName)
	dockerCmd(c, "commit", name, repoName)

	before, _ := dockerCmd(c, "inspect", repoName)

	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", repoName),
		exec.Command(dockerBinary, "load"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save and load repo: %s, %v", out, err))

	after, _ := dockerCmd(c, "inspect", repoName)
	c.Assert(before, checker.Equals, after, check.Commentf("inspect is not the same after a save / load"))
}

func (s *DockerSuite) TestSaveWithNoExistImage(c *check.C) {
	testRequires(c, DaemonIsLinux)

	imgName := "foobar-non-existing-image"

	out, _, err := dockerCmdWithError("save", "-o", "test-img.tar", imgName)
	c.Assert(err, checker.NotNil, check.Commentf("save image should fail for non-existing image"))
	c.Assert(out, checker.Contains, fmt.Sprintf("No such image: %s", imgName))
}

func (s *DockerSuite) TestSaveMultipleNames(c *check.C) {
	testRequires(c, DaemonIsLinux)
	repoName := "foobar-save-multi-name-test"

	// Make one image
	dockerCmd(c, "tag", "emptyfs:latest", fmt.Sprintf("%v-one:latest", repoName))

	// Make two images
	dockerCmd(c, "tag", "emptyfs:latest", fmt.Sprintf("%v-two:latest", repoName))

	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", fmt.Sprintf("%v-one", repoName), fmt.Sprintf("%v-two:latest", repoName)),
		exec.Command("tar", "xO", "repositories"),
		exec.Command("grep", "-q", "-E", "(-one|-two)"),
	)
	c.Assert(err, checker.IsNil, check.Commentf("failed to save multiple repos: %s, %v", out, err))
}

func (s *DockerSuite) TestSaveRepoWithMultipleImages(c *check.C) {
	testRequires(c, DaemonIsLinux)
	makeImage := func(from string, tag string) string {
		var (
			out string
		)
		out, _ = dockerCmd(c, "run", "-d", from, "true")
		cleanedContainerID := strings.TrimSpace(out)

		out, _ = dockerCmd(c, "commit", cleanedContainerID, tag)
		imageID := strings.TrimSpace(out)
		return imageID
	}

	repoName := "foobar-save-multi-images-test"
	tagFoo := repoName + ":foo"
	tagBar := repoName + ":bar"

	idFoo := makeImage("busybox:latest", tagFoo)
	idBar := makeImage("busybox:latest", tagBar)

	deleteImages(repoName)

	// create the archive
	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", repoName, "busybox:latest"),
		exec.Command("tar", "t"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save multiple images: %s, %v", out, err))

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var actual []string
	for _, l := range lines {
		if regexp.MustCompile("^[a-f0-9]{64}\\.json$").Match([]byte(l)) {
			actual = append(actual, strings.TrimSuffix(l, ".json"))
		}
	}

	// make the list of expected layers
	out = inspectField(c, "busybox:latest", "Id")
	expected := []string{strings.TrimSpace(out), idFoo, idBar}

	// prefixes are not in tar
	for i := range expected {
		expected[i] = digest.Digest(expected[i]).Hex()
	}

	sort.Strings(actual)
	sort.Strings(expected)
	c.Assert(actual, checker.DeepEquals, expected, check.Commentf("archive does not contains the right layers: got %v, expected %v, output: %q", actual, expected, out))
}

// Issue #6722 #5892 ensure directories are included in changes
func (s *DockerSuite) TestSaveDirectoryPermissions(c *check.C) {
	testRequires(c, DaemonIsLinux)
	layerEntries := []string{"opt/", "opt/a/", "opt/a/b/", "opt/a/b/c"}
	layerEntriesAUFS := []string{"./", ".wh..wh.aufs", ".wh..wh.orph/", ".wh..wh.plnk/", "opt/", "opt/a/", "opt/a/b/", "opt/a/b/c"}

	name := "save-directory-permissions"
	tmpDir, err := ioutil.TempDir("", "save-layers-with-directories")
	c.Assert(err, checker.IsNil, check.Commentf("failed to create temporary directory: %s", err))
	extractionDirectory := filepath.Join(tmpDir, "image-extraction-dir")
	os.Mkdir(extractionDirectory, 0777)

	defer os.RemoveAll(tmpDir)
	_, err = buildImage(name,
		`FROM busybox
	RUN adduser -D user && mkdir -p /opt/a/b && chown -R user:user /opt/a
	RUN touch /opt/a/b/c && chown user:user /opt/a/b/c`,
		true)
	c.Assert(err, checker.IsNil, check.Commentf("%v", err))

	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", name),
		exec.Command("tar", "-xf", "-", "-C", extractionDirectory),
	)
	c.Assert(err, checker.IsNil, check.Commentf("failed to save and extract image: %s", out))

	dirs, err := ioutil.ReadDir(extractionDirectory)
	c.Assert(err, checker.IsNil, check.Commentf("failed to get a listing of the layer directories: %s", err))

	found := false
	for _, entry := range dirs {
		var entriesSansDev []string
		if entry.IsDir() {
			layerPath := filepath.Join(extractionDirectory, entry.Name(), "layer.tar")

			f, err := os.Open(layerPath)
			c.Assert(err, checker.IsNil, check.Commentf("failed to open %s: %s", layerPath, err))

			entries, err := listTar(f)
			for _, e := range entries {
				if !strings.Contains(e, "dev/") {
					entriesSansDev = append(entriesSansDev, e)
				}
			}
			c.Assert(err, checker.IsNil, check.Commentf("encountered error while listing tar entries: %s", err))

			if reflect.DeepEqual(entriesSansDev, layerEntries) || reflect.DeepEqual(entriesSansDev, layerEntriesAUFS) {
				found = true
				break
			}
		}
	}

	c.Assert(found, checker.Equals, true, check.Commentf("failed to find the layer with the right content listing"))

}

// Test loading a weird image where one of the layers is of zero size.
// The layer.tar file is actually zero bytes, no padding or anything else.
// See issue: 18170
func (s *DockerSuite) TestLoadZeroSizeLayer(c *check.C) {
	testRequires(c, DaemonIsLinux)

	dockerCmd(c, "load", "-i", "fixtures/load/emptyLayer.tar")
}

func (s *DockerSuite) TestSaveLoadParents(c *check.C) {
	testRequires(c, DaemonIsLinux)

	makeImage := func(from string, addfile string) string {
		var (
			out string
		)
		out, _ = dockerCmd(c, "run", "-d", from, "touch", addfile)
		cleanedContainerID := strings.TrimSpace(out)

		out, _ = dockerCmd(c, "commit", cleanedContainerID)
		imageID := strings.TrimSpace(out)

		dockerCmd(c, "rm", "-f", cleanedContainerID)
		return imageID
	}

	idFoo := makeImage("busybox", "foo")
	idBar := makeImage(idFoo, "bar")

	tmpDir, err := ioutil.TempDir("", "save-load-parents")
	c.Assert(err, checker.IsNil)
	defer os.RemoveAll(tmpDir)

	c.Log("tmpdir", tmpDir)

	outfile := filepath.Join(tmpDir, "out.tar")

	dockerCmd(c, "save", "-o", outfile, idBar, idFoo)
	dockerCmd(c, "rmi", idBar)
	dockerCmd(c, "load", "-i", outfile)

	inspectOut := inspectField(c, idBar, "Parent")
	c.Assert(inspectOut, checker.Equals, idFoo)

	inspectOut = inspectField(c, idFoo, "Parent")
	c.Assert(inspectOut, checker.Equals, "")
}

func (s *DockerSuite) TestSaveLoadNoTag(c *check.C) {
	testRequires(c, DaemonIsLinux)

	name := "saveloadnotag"

	_, err := buildImage(name, "FROM busybox\nENV foo=bar", true)
	c.Assert(err, checker.IsNil, check.Commentf("%v", err))

	id := inspectField(c, name, "Id")

	// Test to make sure that save w/o name just shows imageID during load
	out, _, err := runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", id),
		exec.Command(dockerBinary, "load"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save and load repo: %s, %v", out, err))

	// Should not show 'name' but should show the image ID during the load
	c.Assert(out, checker.Not(checker.Contains), "Loaded image: ")
	c.Assert(out, checker.Contains, "Loaded image ID:")
	c.Assert(out, checker.Contains, id)

	// Test to make sure that save by name shows that name during load
	out, _, err = runCommandPipelineWithOutput(
		exec.Command(dockerBinary, "save", name),
		exec.Command(dockerBinary, "load"))
	c.Assert(err, checker.IsNil, check.Commentf("failed to save and load repo: %s, %v", out, err))
	c.Assert(out, checker.Contains, "Loaded image: "+name+":latest")
	c.Assert(out, checker.Not(checker.Contains), "Loaded image ID:")
}
