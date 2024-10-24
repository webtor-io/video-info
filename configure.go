package main

import (
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	s "github.com/webtor-io/video-info/services"
	"github.com/webtor-io/video-info/services/osdb"
	"github.com/webtor-io/video-info/services/redis"
	"github.com/webtor-io/video-info/services/s3"
)

func configure(app *cli.App) {
	app.Flags = []cli.Flag{}
	app.Flags = cs.RegisterProbeFlags(app.Flags)
	app.Flags = s.RegisterWebFlags(app.Flags)
	app.Flags = osdb.RegisterOSDBClientFlags(app.Flags)
	app.Flags = cs.RegisterRedisClientFlags(app.Flags)
	app.Flags = cs.RegisterS3ClientFlags(app.Flags)
	app.Flags = s3.RegisterS3StorageFlags(app.Flags)

	app.Action = run
}

func run(c *cli.Context) error {
	// Setting S3Client
	s3cl := cs.NewS3Client(c, &http.Client{
		Timeout: time.Second * 60,
	})

	// Setting S3Storage
	s3st := s3.NewS3Storage(c, s3cl)

	// Setting redisClient
	redisClient := cs.NewRedisClient(c)

	// Setting cachePool
	cachePool := redis.NewCachePool(redisClient)

	// Setting HTTP Client
	httpClient := http.DefaultClient

	// Setting OSDB Client
	client := osdb.NewClient(c, httpClient)

	// Setting searchPool
	searchPool := s.NewSearchPool(client)

	// Setting imdbSearchPool
	imdbSearchPool := s.NewIMDBSearchPool(client)

	// Setting subsPool
	subsPool := s.NewSubsPool(client, s3st)

	// Setting ProbeService
	probe := cs.NewProbe(c)
	defer probe.Close()

	// Setting WebService
	web := s.NewWeb(c, searchPool, imdbSearchPool, subsPool, cachePool)
	defer web.Close()

	// Setting ServeService
	serve := cs.NewServe(probe, web)

	// And SERVE!
	err := serve.Serve()
	if err != nil {
		log.WithError(err).Error("Got server error")
	}
	return err
}
