package client

import (
	"io"
	"net/url"

	"github.com/docker/docker/opts"
)

// ImageSave retrieves one or more images from the docker host as a io.ReadCloser.
// It's up to the caller to store the images and close the stream.
func (cli *Client) ImageSave(imageIDs []string, excludeIDs opts.ListOpts) (io.ReadCloser, error) {
	query := url.Values{
		"names": imageIDs,
	}
	for _, img := range excludeIDs.GetAll() {
		query.Add("exclude", img)
	}

	resp, err := cli.get("/images/get", query, nil)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}
