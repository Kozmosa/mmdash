package artifact

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

type corsConfiguration struct {
	XMLName xml.Name   `xml:"CORSConfiguration"`
	XMLNS   string     `xml:"xmlns,attr"`
	Rules   []corsRule `xml:"CORSRule"`
}

type corsRule struct {
	AllowedHeaders []string `xml:"AllowedHeader"`
	AllowedMethods []string `xml:"AllowedMethod"`
	AllowedOrigins []string `xml:"AllowedOrigin"`
	ExposeHeaders  []string `xml:"ExposeHeader"`
	MaxAgeSeconds  int      `xml:"MaxAgeSeconds"`
}

// EnsureS3Bucket creates the configured bucket when absent and installs the
// exact browser-origin CORS policy required for direct multipart transfers.
// It is intended for deployment initialization, not Core request handling.
func EnsureS3Bucket(
	ctx context.Context,
	storageConfig S3BlobStoreConfig,
	webOrigin string,
) error {
	origin, err := url.Parse(webOrigin)
	if err != nil ||
		origin.Host == "" ||
		origin.User != nil ||
		(origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.Path != "" ||
		origin.RawQuery != "" ||
		origin.Fragment != "" {
		return fmt.Errorf("Artifact Web origin must be an HTTP(S) origin")
	}
	store, err := NewS3BlobStore(storageConfig)
	if err != nil {
		return err
	}
	err = store.client.Client.MakeBucket(
		ctx,
		store.bucket,
		minio.MakeBucketOptions{Region: storageConfig.Region},
	)
	if err != nil &&
		!isS3Code(err, "BucketAlreadyOwnedByYou") &&
		!isS3Code(err, "BucketAlreadyExists") {
		exists, existsErr := store.client.Client.BucketExists(ctx, store.bucket)
		if existsErr != nil || !exists {
			return fmt.Errorf("ensure object storage bucket: %w", err)
		}
	}
	if storageConfig.Backend == "minio" {
		return verifyMinIOCORS(
			ctx,
			storageConfig.Endpoint,
			store.bucket,
			webOrigin,
		)
	}

	contents, err := xml.Marshal(corsConfiguration{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		Rules: []corsRule{{
			AllowedHeaders: []string{"content-type", "x-amz-*"},
			AllowedMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut},
			AllowedOrigins: []string{webOrigin},
			ExposeHeaders:  []string{"ETag", "x-amz-checksum-sha256"},
			MaxAgeSeconds:  3600,
		}},
	})
	if err != nil {
		return fmt.Errorf("encode object storage CORS policy: %w", err)
	}
	if err := putBucketCORS(ctx, store.client.Client, store.bucket, contents); err != nil {
		return err
	}
	return verifyBucketCORS(ctx, store.client.Client, store.bucket, webOrigin)
}

func verifyMinIOCORS(
	ctx context.Context,
	endpoint string,
	bucket string,
	webOrigin string,
) error {
	target, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse MinIO CORS endpoint: %w", err)
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/" + bucket + "/cors-probe"
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodOptions,
		target.String(),
		nil,
	)
	if err != nil {
		return fmt.Errorf("create MinIO CORS preflight: %w", err)
	}
	request.Header.Set("Origin", webOrigin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	request.Header.Set(
		"Access-Control-Request-Headers",
		"content-type,x-amz-content-sha256",
	)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("check MinIO CORS preflight: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 ||
		response.StatusCode >= 300 ||
		response.Header.Get("Access-Control-Allow-Origin") != webOrigin ||
		!strings.Contains(
			response.Header.Get("Access-Control-Allow-Methods"),
			http.MethodPut,
		) {
		return fmt.Errorf("MinIO CORS verification failed")
	}
	return nil
}

func putBucketCORS(
	ctx context.Context,
	client *minio.Client,
	bucket string,
	contents []byte,
) error {
	query := url.Values{"cors": []string{""}}
	signed, err := client.Presign(
		ctx,
		http.MethodPut,
		bucket,
		"",
		5*time.Minute,
		query,
	)
	if err != nil {
		return fmt.Errorf("sign object storage CORS update: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		signed.String(),
		bytes.NewReader(contents),
	)
	if err != nil {
		return fmt.Errorf("create object storage CORS update: %w", err)
	}
	digest := md5.Sum(contents)
	request.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(digest[:]))
	request.Header.Set("Content-Type", "application/xml")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("update object storage CORS: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf(
			"update object storage CORS returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	return nil
}

func verifyBucketCORS(
	ctx context.Context,
	client *minio.Client,
	bucket string,
	webOrigin string,
) error {
	signed, err := client.Presign(
		ctx,
		http.MethodGet,
		bucket,
		"",
		time.Minute,
		url.Values{"cors": []string{""}},
	)
	if err != nil {
		return fmt.Errorf("sign object storage CORS read: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.String(), nil)
	if err != nil {
		return fmt.Errorf("create object storage CORS read: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("read object storage CORS: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read object storage CORS response: %w", err)
	}
	if response.StatusCode < 200 ||
		response.StatusCode >= 300 ||
		!bytes.Contains(body, []byte(webOrigin)) {
		return fmt.Errorf("object storage CORS verification failed")
	}
	return nil
}
