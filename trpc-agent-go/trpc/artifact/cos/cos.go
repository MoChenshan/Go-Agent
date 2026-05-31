// Package cos provides COS (Cloud Object Storage) client functionality for trpc-agent-go.
package cos

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	databasecos "git.code.oa.com/trpc-go/trpc-database/cos"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc" // Import for side effects to register trpc components.
	sdkcos "github.com/tencentyun/cos-go-sdk-v5"
	agentcos "trpc.group/trpc-go/trpc-agent-go/artifact/cos"
)

func init() {
	agentcos.SetClientBuilder(trpcCosClientBuilder)
}

func trpcCosClientBuilder(name string, bucketURL string, opts ...agentcos.Option) (any, error) {
	// databasecos.Conf and client.Option should be configured at trpc_go.yaml
	return newClient(databasecos.NewClientProxy(name, databasecos.Conf{})), nil
}

// cosClient implements agentcos.client interface
type cosClient struct {
	client databasecos.Client
}

func newClient(client databasecos.Client) *cosClient {
	return &cosClient{client: client}
}

func (c *cosClient) GetBucket(ctx context.Context, prefix string) (*sdkcos.BucketGetResult, error) {
	bts, err := c.client.GetBucket(ctx, databasecos.WithPrefix(prefix))
	if err != nil {
		return nil, err
	}
	var result sdkcos.BucketGetResult
	err = xml.Unmarshal(bts, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal bucket get result: %w", err)
	}
	return &result, nil
}
func (c *cosClient) PutObject(ctx context.Context, name string, content io.Reader, mimeType string) error {
	bts, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	_, err = c.client.PutObject(ctx, bts, name, databasecos.WithCustomHeader("Content-Type", mimeType))
	return err
}
func (c *cosClient) GetObject(ctx context.Context, name string) (body io.ReadCloser, header http.Header, err error) {
	bts, err := c.client.GetObject(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get object: %w", err)
	}

	header, err = c.client.HeadObject(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to head object: %w", err)
	}

	return io.NopCloser(bytes.NewReader(bts)), header, nil
}

func (c *cosClient) DeleteObject(ctx context.Context, name string) error {
	return c.client.DelObject(ctx, name)
}
