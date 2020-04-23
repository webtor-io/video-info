package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	s "github.com/webtor-io/video-info/services"
	"github.com/webtor-io/video-info/services/osdb"
	"github.com/webtor-io/video-info/services/redis"
)

func configure(app *cli.App) {
	app.Flags = []cli.Flag{}
	cs.RegisterProbeFlags(app)
	s.RegisterWebFlags(app)
	cs.RegisterRedisClientFlags(app)

	app.Action = run
}

func run(c *cli.Context) error {
	// Setting redisClient
	redisClient := cs.NewRedisClient(c)

	// Setting cachePool
	cachePool := redis.NewCachePool(redisClient)

	// Setting OSDB Client
	client := osdb.NewClient(c)

	// Setting searchPool
	searchPool := osdb.NewSearchPool(client)

	// Setting imdbSearchPool
	imdbSearchPool := osdb.NewIMDBSearchPool(client)

	// Setting subsPool
	subsPool := osdb.NewSubsPool()

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
