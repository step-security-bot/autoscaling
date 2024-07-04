package billing

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/azurite"
	"github.com/stretchr/testify/require"
)

type container struct {
	c    *gnomock.Container
	Host string
}

// Azurite is an emulator for azure blob, queue storage, table storage.
// https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite
func startAzuriteContainer(version string) (*container, error) {
	p := azurite.Preset(
		azurite.WithVersion(version),
	)
	c, err := gnomock.Start(p, gnomock.WithHealthCheck(func(ctx context.Context, c *gnomock.Container) error {
		_, err := net.Dial("tcp", fmt.Sprintf("%s:%d", realHostname(c.Host), c.Ports["blob"].Port))
		return err
	}), gnomock.WithTimeout(time.Minute))
	if err != nil {
		return nil, err
	}
	r := &container{c: c, Host: realHostname(c.Host)}
	return r, err
}

// realHostname changes host to host.docker.internal if ran inside docker (GNOMOCK_ENV=gnomockd)
// so it would be able to connect to port, opened on host machine (or forwarded to other container)
func realHostname(host string) string {
	gnomockEnv := os.Getenv("GNOMOCK_ENV")
	if gnomockEnv == "gnomockd" {
		// we could patch it with arbitrary host, but inside gnomock
		// there is already GNOMOCK_ENV is used, so we will follow the same approach
		return "host.docker.internal"
	}
	return host
}

func TestAzureClient_send(t *testing.T) {
	if testing.Short() {
		t.Skip("skip long-running test in short mode")
	}
	t.Parallel()
	type input struct {
		ctx     context.Context
		cfg     AzureBlobStorageClientConfig
		payload []byte
		traceID TraceID
		client  *AzureClient
	}
	type output struct {
		ctx context.Context
		err error
		c   *AzureClient
	}
	tests := []struct {
		name string
		when func(t *testing.T, i *input)
		then func(t *testing.T, o output)
	}{
		{
			name: "no container exists",
			when: func(t *testing.T, i *input) {},
			then: func(t *testing.T, o output) {
				require.Error(t, o.err)
				var azErr AzureError
				require.ErrorAs(t, o.err, &azErr)
				rErr := &azcore.ResponseError{} //nolint:exhaustruct // OK for tests
				require.ErrorAs(t, o.err, &rErr)
				require.Equal(t, 404, rErr.StatusCode)
			},
		},
		{
			name: "can write then read it",
			when: func(t *testing.T, i *input) {
				_, err := i.client.c.CreateContainer(i.ctx, i.cfg.Container,
					&azblob.CreateContainerOptions{}, //nolint:exhaustruct // OK for tests
				)
				require.NoError(t, err)
			},
			then: func(t *testing.T, o output) {
				require.NoError(t, o.err)
				b := make([]byte, 1000)
				const expectedText = "hello, billing data is here"
				read, err := o.c.c.DownloadBuffer(o.ctx, "test-container", "test-blob-name", b,
					&azblob.DownloadBufferOptions{}, //nolint:exhaustruct // OK for tests
				)
				b = b[0:read]
				require.NoError(t, err)
				b, err = bytesFromStorage(b)
				require.NoError(t, err)
				require.Equal(t, b, []byte(expectedText))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			azureBlobStorage, err := startAzuriteContainer("3.30.0")
			require.NoError(t, err)

			endpoint := fmt.Sprintf("http://%s:%d/devstoreaccount1", azureBlobStorage.Host, azureBlobStorage.c.Ports["blob"].Port)

			// feel free to override in tests.
			i := &input{ //nolint:exhaustruct // OK for tests
				payload: []byte("hello, billing data is here"),
				ctx:     ctx,
				cfg: AzureBlobStorageClientConfig{
					Endpoint: endpoint,
					getClient: func() (*azblob.Client, error) {
						// Using well known credentials,
						// see https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite
						shKey, err := azblob.NewSharedKeyCredential(
							"devstoreaccount1",
							"Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==")
						if err != nil {
							panic(err)
						}

						client, err := azblob.NewClientWithSharedKeyCredential(endpoint, shKey, nil)
						if err != nil {
							panic(err)
						}
						return client, nil
					},
					PrefixInContainer: "test-prefix",
					Container:         "test-container",
					generateKey: func() string {
						return "test-blob-name"
					},
				},
			}
			c, err := NewAzureBlobStorageClient(i.cfg)
			require.NoError(t, err)
			i.client = c
			tt.when(t, i)

			err = i.client.send(ctx, i.payload, i.traceID)

			tt.then(t, output{
				err: err,
				c:   i.client,
				ctx: ctx,
			})
		})
	}
}

func TestBytesForBilling(t *testing.T) {
	const expectedText = "hello, billing data is here"
	billing, err := compress([]byte(expectedText))
	require.NoError(t, err)
	storage, err := bytesFromStorage(billing)
	require.NoError(t, err)
	require.Equal(t, expectedText, string(storage))
}

func bytesFromStorage(i []byte) ([]byte, error) {
	gzR, err := gzip.NewReader(bytes.NewBuffer(i))
	if err != nil {
		return nil, err
	}
	var resB bytes.Buffer
	_, err = resB.ReadFrom(gzR)
	if err != nil {
		return nil, err
	}
	err = gzR.Close() // Have to close it before reading the buffer
	if err != nil {
		return nil, err
	}
	return resB.Bytes(), nil
}
