package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type fakeS3 struct {
	bucketCheckErr error
	headObjectErr  error
	putObjectErr   error
	objects        []types.Object
	created        bool
	putObjectKey   string
	listed         bool
	deletedKeys    []types.ObjectIdentifier
	bucketDeleted  bool
}

func (f *fakeS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, f.bucketCheckErr
}

func (f *fakeS3) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	f.created = true
	return &s3.CreateBucketOutput{}, nil
}

func (f *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putObjectKey = *input.Key
	return &s3.PutObjectOutput{}, f.putObjectErr
}

func (f *fakeS3) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{}, f.headObjectErr
}

func (f *fakeS3) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.listed = true
	return &s3.ListObjectsV2Output{Contents: f.objects}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	f.deletedKeys = input.Delete.Objects
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	f.bucketDeleted = true
	return &s3.DeleteBucketOutput{}, nil
}

func TestEnsureSSMBucketMarksOnlyBucketsItCreates(t *testing.T) {
	existing := &fakeS3{}
	if err := (&Client{S3: existing}).EnsureSSMBucket(context.Background(), "operator-bucket"); err != nil {
		t.Fatalf("ensure existing bucket: %v", err)
	}
	if existing.created || existing.putObjectKey != "" {
		t.Fatal("existing bucket was created or marked as DreadGOAD-managed")
	}

	created := &fakeS3{bucketCheckErr: &smithy.GenericAPIError{Code: "NotFound"}}
	if err := (&Client{S3: created, Region: "us-east-1"}).EnsureSSMBucket(context.Background(), "new-bucket"); err != nil {
		t.Fatalf("ensure new bucket: %v", err)
	}
	if !created.created || created.putObjectKey != managedBucketMarkerKey {
		t.Fatalf("new bucket created=%v marker=%q, want created and managed marker", created.created, created.putObjectKey)
	}
}

func TestEnsureSSMBucketRemovesUnmarkedBucketWhenMarkerWriteFails(t *testing.T) {
	fake := &fakeS3{
		bucketCheckErr: &smithy.GenericAPIError{Code: "NotFound"},
		putObjectErr:   errors.New("write denied"),
	}
	err := (&Client{S3: fake}).EnsureSSMBucket(context.Background(), "new-bucket")
	if err == nil || !fake.bucketDeleted {
		t.Fatalf("err=%v bucketDeleted=%v, want error and cleanup", err, fake.bucketDeleted)
	}
}

func TestDeleteSSMBucketPreservesUnmanagedBucket(t *testing.T) {
	for _, tc := range []struct {
		name          string
		headObjectErr error
	}{
		{name: "missing marker", headObjectErr: &smithy.GenericAPIError{Code: "NoSuchKey"}},
		{name: "generic not found", headObjectErr: &smithy.GenericAPIError{Code: "NotFound"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeS3{headObjectErr: tc.headObjectErr}
			if err := (&Client{S3: fake}).DeleteSSMBucket(context.Background(), "operator-bucket"); err != nil {
				t.Fatalf("delete unmanaged bucket: %v", err)
			}
			if fake.listed || fake.bucketDeleted {
				t.Fatalf("unmanaged bucket listed=%v deleted=%v, want untouched", fake.listed, fake.bucketDeleted)
			}
		})
	}
}

func TestDeleteSSMBucketDeletesManagedBucketContentsAndBucket(t *testing.T) {
	fake := &fakeS3{
		objects: []types.Object{
			{Key: Ptr("first")},
			{Key: Ptr("second")},
		},
	}
	if err := (&Client{S3: fake}).DeleteSSMBucket(context.Background(), "managed-bucket"); err != nil {
		t.Fatalf("delete managed bucket: %v", err)
	}
	if !fake.listed || len(fake.deletedKeys) != 2 || !fake.bucketDeleted {
		t.Fatalf("listed=%v deleted keys=%d bucket deleted=%v", fake.listed, len(fake.deletedKeys), fake.bucketDeleted)
	}
}
