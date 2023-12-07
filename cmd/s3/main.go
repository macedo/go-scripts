package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

var ()

func main() {
	bucket := flag.String("bucket", "", "bucket name")
	configProfile := flag.String("config-profile", "", "aws config profile")
	files := flag.String("files", "", "file path or path do dir that contains files to be uploaded")
	region := flag.String("region", "sa-east-1", "aws region")
	flag.Parse()

	if *bucket == "" || *configProfile == "" || *files == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithSharedConfigProfile(*configProfile),
		config.WithRegion(*region),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := s3.NewFromConfig(cfg)

	exists, err := bucketExists(ctx, client, *bucket)
	if err != nil {
		log.Fatal(err)
	}

	if !exists {
		if err := createBucket(ctx, client, *bucket, *region); err != nil {
			log.Fatal(err)
		}
	}

	filenames := []string{}

	fileInfo, err := os.Stat(*files)
	if err != nil {
		log.Fatal(err)
	}

	path, err := filepath.Abs(*files)
	if err != nil {
		log.Fatal(err)
	}

	if fileInfo.IsDir() {
		entries, err := os.ReadDir(*files)
		if err != nil {
			log.Fatal(err)
		}

		for _, e := range entries {
			filenames = append(filenames, filepath.Join(path, e.Name()))
		}
	} else {
		filenames = append(filenames, path)
	}

	doneCh := make(chan struct{})
	errCh := make(chan error)
	filesCh := make(chan string)
	resCh := make(chan string)

	wg := sync.WaitGroup{}

	go func() {
		defer close(filesCh)
		for _, fname := range filenames {
			filesCh <- fname
		}
	}()

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fname := range filesCh {
				if err := uploadFile(ctx, client, *bucket, fname); err != nil {
					errCh <- err
					return
				}

				resCh <- fmt.Sprintf("%s uploaded with success", fname)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(doneCh)
	}()

	for {
		select {
		case err := <-errCh:
			log.Println(err)
		case data := <-resCh:
			log.Println(data)
		case <-doneCh:
			return
		}
	}
}

func bucketExists(ctx context.Context, client *s3.Client, name string) (bool, error) {
	_, err := client.HeadBucket(context.TODO(), &s3.HeadBucketInput{
		Bucket: aws.String(name),
	})
	exists := true
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) {
			switch apiError.(type) {
			case *types.NotFound:
				log.Printf("Bucket %v is available.\n", name)
				exists = false
				err = nil
			default:
				log.Printf("Either you don't have access to bucket %v or another error occurred. "+
					"Here's what happened: %v\n", name, err)
			}
		}
	} else {
		log.Printf("Bucket %v exists and you already own it.", name)
	}

	return exists, err
}

func createBucket(ctx context.Context, client *s3.Client, name string, region string) error {
	_, err := client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: aws.String(name),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		},
	})
	if err != nil {
		log.Printf("Couldn't create bucket %v in Region %v. Here's why: %v\n", name, region, err)
	}

	log.Printf("Bucket %s created", name)
	return err
}

func uploadFile(ctx context.Context, client *s3.Client, bucket, path string) error {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("Couldn't open file %v to upload. Here's why: %v\n", path, err)
	} else {
		defer file.Close()
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(filepath.Base(path)),
			Body:   file,
		})
		if err != nil {
			log.Printf("Couldn't upload file %v to %v:%v. Here's why: %v\n", path, bucket, path, err)
		}
	}

	return err
}
