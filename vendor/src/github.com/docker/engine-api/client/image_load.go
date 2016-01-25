package client

import (
	"io"
	"net/url"

	"github.com/docker/engine-api/types"
)

// ImageLoad loads an image in the docker host from the client host.
// It's up to the caller to close the io.ReadCloser returned by
// this function.
func (cli *Client) ImageLoad(input io.Reader, printExcludes bool) (types.ImageLoadResponse, error) {
	query := url.Values{}
	if printExcludes {
		query.Set("printExcludes", "1")
	}
	resp, err := cli.postRaw("/images/load", query, input, nil)
	if err != nil {
		return types.ImageLoadResponse{}, err
	}
	return types.ImageLoadResponse{
		Body: resp.body,
		JSON: resp.header.Get("Content-Type") == "application/json",
	}, nil
}
