// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
)

// InitParams contains the initialization parameters for Init(). It is
// usually filled in by the flags parser passed into AddFlags().
type InitParams struct {
	// Whether to print debug messages.
	Debug bool
	// If non-empty, where to write a CPU profile.
	CPUProfile string

	// If non-empty, the host:port of the block server. If empty,
	// a default value is used depending on the run mode.
	BServerAddr string
	// If non-empty the host:port of the metadata server. If
	// empty, a default value is used depending on the run mode.
	MDServerAddr string

	// If true, use in-memory servers and ignore BServerAddr,
	// MDServerAddr, and ServerRootDir.
	ServerInMemory bool
	// If non-empty, use on-disk servers and ignore BServerAddr
	// and MDServerAddr.
	ServerRootDir string
	// Fake local user name. If non-empty, either ServerInMemory
	// must be true or ServerRootDir must be non-empty.
	LocalUser string

	// TLFValidDuration is the duration that TLFs are valid
	// before marked for lazy revalidation.
	TLFValidDuration time.Duration

	// LogToFile if true, logs to a default file location.
	LogToFile bool

	// LogFileConfig tells us where to log and rotation config.
	LogFileConfig logger.LogFileConfig
}

var libkbOnce sync.Once

func initLibkb() {
	libkbOnce.Do(func() {
		libkb.G.Init()
		libkb.G.ConfigureConfig()
		libkb.G.ConfigureLogging()
		libkb.G.ConfigureCaches()
		libkb.G.ConfigureMerkleClient()
	})
}

func getRunMode() libkb.RunMode {
	// Initialize libkb.G.Env.
	initLibkb()
	return libkb.G.Env.GetRunMode()
}

// GetDefaultBServer returns the default value for the -bserver flag.
func GetDefaultBServer() string {
	switch getRunMode() {
	case libkb.StagingRunMode:
		return "bserver.dev.keybase.io:443"
	case libkb.ProductionRunMode:
		return "bserver.kbfs.keybase.io:443"
	default:
		return ""
	}
}

// GetDefaultMDServer returns the default value for the -mdserver flag.
func GetDefaultMDServer() string {
	switch getRunMode() {
	case libkb.StagingRunMode:
		return "mdserver.dev.keybase.io:443"
	case libkb.ProductionRunMode:
		return "mdserver.kbfs.keybase.io:443"
	default:
		return ""
	}
}

func defaultLogPath() string {
	// TODO is there a better way to get G here?
	return filepath.Join(libkb.G.Env.GetLogDir(), libkb.KBFSLogFileName)
}

// AddFlags adds libkbfs flags to the given FlagSet. Returns an
// InitParams that will be filled in once the given FlagSet is parsed.
func AddFlags(flags *flag.FlagSet) *InitParams {
	var params InitParams
	flags.BoolVar(&params.Debug, "debug", BoolForString(os.Getenv("KBFS_DEBUG")), "Print debug messages")
	flags.StringVar(&params.CPUProfile, "cpuprofile", "", "write cpu profile to file")

	flags.StringVar(&params.BServerAddr, "bserver", GetDefaultBServer(), "host:port of the block server")
	flags.StringVar(&params.MDServerAddr, "mdserver", GetDefaultMDServer(), "host:port of the metadata server")

	flags.BoolVar(&params.ServerInMemory, "server-in-memory", false, "use in-memory server (and ignore -bserver, -mdserver, and -server-root)")
	flags.StringVar(&params.ServerRootDir, "server-root", "", "directory to put local server files (and ignore -bserver and -mdserver)")
	flags.StringVar(&params.LocalUser, "localuser", "", "fake local user (used only with -server-in-memory or -server-root)")
	flags.DurationVar(&params.TLFValidDuration, "tlf-valid", tlfValidDurationDefault, "time tlfs are valid before redoing identification")
	flags.BoolVar(&params.LogToFile, "log-to-file", false, fmt.Sprintf("Log to default file: %s", defaultLogPath()))
	flags.StringVar(&params.LogFileConfig.Path, "log-file", "", "Path to log file")
	flags.DurationVar(&params.LogFileConfig.MaxAge, "log-file-max-age", 30*24*time.Hour, "Maximum age of a log file before rotation")
	params.LogFileConfig.MaxSize = 128 * 1024 * 1024
	flag.Var(SizeFlag{&params.LogFileConfig.MaxSize}, "log-file-max-size", "Maximum size of a log file before rotation")
	// The default is to *DELETE* old log files for kbfs.
	flag.IntVar(&params.LogFileConfig.MaxKeepFiles, "log-file-max-keep-files", 3, "Maximum number of log files for this service, older ones are deleted. 0 for infinite.")
	return &params
}

func makeMDServer(config Config, serverInMemory bool, serverRootDir, mdserverAddr string) (
	MDServer, error) {
	if serverInMemory {
		// local in-memory MD server
		return NewMDServerMemory(config)
	}

	if len(serverRootDir) > 0 {
		// local persistent MD server
		handlePath := filepath.Join(serverRootDir, "kbfs_handles")
		mdPath := filepath.Join(serverRootDir, "kbfs_md")
		branchPath := filepath.Join(serverRootDir, "kbfs_branches")
		return NewMDServerLocal(
			config, handlePath, mdPath, branchPath)
	}

	if len(mdserverAddr) == 0 {
		return nil, errors.New("Empty MD server address")
	}

	// remote MD server. this can't fail. reconnection attempts
	// will be automatic.
	mdServer := NewMDServerRemote(config, mdserverAddr)
	return mdServer, nil
}

func makeKeyServer(config Config, serverInMemory bool, serverRootDir, keyserverAddr string) (
	KeyServer, error) {
	if serverInMemory {
		// local in-memory key server
		return NewKeyServerMemory(config)
	}

	if len(serverRootDir) > 0 {
		// local persistent key server
		keyPath := filepath.Join(serverRootDir, "kbfs_key")
		return NewKeyServerLocal(config, keyPath)
	}

	if len(keyserverAddr) == 0 {
		return nil, errors.New("Empty key server address")
	}

	// currently the MD server also acts as the key server.
	keyServer, ok := config.MDServer().(KeyServer)
	if !ok {
		return nil, errors.New("MD server is not a key server")
	}
	return keyServer, nil
}

func makeBlockServer(config Config, serverInMemory bool, serverRootDir, bserverAddr string, log logger.Logger) (
	BlockServer, error) {
	if serverInMemory {
		// local in-memory block server
		return NewBlockServerMemory(config), nil
	}

	if len(serverRootDir) > 0 {
		// local persistent block server
		blockPath := filepath.Join(serverRootDir, "kbfs_block")
		return NewBlockServerDir(config, blockPath), nil
	}

	if len(bserverAddr) == 0 {
		return nil, errors.New("Empty block server address")
	}

	log.Debug("Using remote bserver %s", bserverAddr)
	return NewBlockServerRemote(config, bserverAddr), nil
}

func makeKeybaseDaemon(config Config, serverInMemory bool, serverRootDir string, localUser libkb.NormalizedUsername, codec Codec, log logger.Logger, debug bool) (KeybaseDaemon, error) {
	if len(localUser) == 0 {
		libkb.G.ConfigureSocketInfo()
		return NewKeybaseDaemonRPC(config, libkb.G, log, debug), nil
	}

	users := []libkb.NormalizedUsername{"strib", "max", "chris", "fred"}
	userIndex := -1
	for i := range users {
		if localUser == users[i] {
			userIndex = i
			break
		}
	}
	if userIndex < 0 {
		return nil, fmt.Errorf("user %s not in list %v", localUser, users)
	}

	localUsers := MakeLocalUsers(users)

	// TODO: Auto-generate these, too?
	localUsers[0].Asserts = []string{"github:strib"}
	localUsers[1].Asserts = []string{"twitter:maxtaco"}
	localUsers[2].Asserts = []string{"twitter:malgorithms"}
	localUsers[3].Asserts = []string{"twitter:fakalin"}

	localUID := localUsers[userIndex].UID

	if serverInMemory {
		return NewKeybaseDaemonMemory(localUID, localUsers, codec), nil
	}

	if len(serverRootDir) > 0 {
		favPath := filepath.Join(serverRootDir, "kbfs_favs")
		return NewKeybaseDaemonDisk(localUID, localUsers, favPath, codec)
	}

	return nil, errors.New("Can't user localuser without a local server")
}

// InitLog sets up logging switching to a log file if necessary.
// Returns a valid logger even on error, which are non-fatal, thus
// errors from this function may be ignored.
// Possible errors are logged to the logger returned.
func InitLog(params InitParams) (logger.Logger, error) {
	var err error
	log := logger.NewWithCallDepth("kbfs", 1)

	// Set log file to default if log-to-file was specified
	if params.LogToFile {
		if params.LogFileConfig.Path != "" {
			return nil, fmt.Errorf("log-to-file and log-file flags can't be specified together")
		}
		params.LogFileConfig.Path = defaultLogPath()
	}

	if params.LogFileConfig.Path != "" {
		err = logger.SetLogFileConfig(&params.LogFileConfig)
	}

	log.Configure("", params.Debug, "")
	log.Info("KBFS version %s", VersionString())

	if err != nil {
		log.Warning("Failed to setup log file %q: %v", params.LogFileConfig.Path, err)
	}

	return log, err
}

// Init initializes a config and returns it.
//
// onInterruptFn is called whenever an interrupt signal is received
// (e.g., if the user hits Ctrl-C).
//
// Init should be called at the beginning of main. Shutdown (see
// below) should then be called at the end of main (usually via
// defer).
func Init(params InitParams, onInterruptFn func(), log logger.Logger) (Config, error) {
	initLibkb()

	localUser := libkb.NewNormalizedUsername(params.LocalUser)

	if params.CPUProfile != "" {
		// Let the GC/OS clean up the file handle.
		f, err := os.Create(params.CPUProfile)
		if err != nil {
			return nil, err
		}
		pprof.StartCPUProfile(f)
	}

	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)
	go func() {
		_ = <-interruptChan

		if onInterruptFn != nil {
			onInterruptFn()
		}

		os.Exit(1)
	}()

	config := NewConfigLocal()

	// 512K blocks by default, block changes embedded max == 8K.
	// Block size was chosen somewhat arbitrarily by trying to
	// minimize the overall size of the history written by a user when
	// appending 1KB writes to a file, up to a 1GB total file.  Here
	// is the output of a simple script that approximates that
	// calculation:
	//
	// Total history size for 0065536-byte blocks: 1134341128192 bytes
	// Total history size for 0131072-byte blocks: 618945052672 bytes
	// Total history size for 0262144-byte blocks: 412786622464 bytes
	// Total history size for 0524288-byte blocks: 412786622464 bytes
	// Total history size for 1048576-byte blocks: 618945052672 bytes
	// Total history size for 2097152-byte blocks: 1134341128192 bytes
	// Total history size for 4194304-byte blocks: 2216672886784 bytes
	bsplitter, err := NewBlockSplitterSimple(512*1024, 8*1024,
		config.Codec())
	if err != nil {
		return nil, err
	}
	config.SetBlockSplitter(bsplitter)

	if registry := config.MetricsRegistry(); registry != nil {
		keyCache := config.KeyCache()
		keyCache = NewKeyCacheMeasured(keyCache, registry)
		config.SetKeyCache(keyCache)
	}

	// Set logging
	config.SetLoggerMaker(func(module string) logger.Logger {
		mname := "kbfs"
		if module != "" {
			mname += fmt.Sprintf("(%s)", module)
		}
		// Add log depth so that context-based messages get the right
		// file printed out.
		lg := logger.NewWithCallDepth(mname, 1)
		if params.Debug {
			// Turn on debugging.  TODO: allow a proper log file and
			// style to be specified.
			lg.Configure("", true, "")
		}
		return lg
	})

	config.SetTLFValidDuration(params.TLFValidDuration)

	kbfsOps := NewKBFSOpsStandard(config)
	config.SetKBFSOps(kbfsOps)
	config.SetNotifier(kbfsOps)
	config.SetKeyManager(NewKeyManagerStandard(config))
	config.SetMDOps(NewMDOpsStandard(config))

	mdServer, err := makeMDServer(
		config, params.ServerInMemory, params.ServerRootDir, params.MDServerAddr)
	if err != nil {
		return nil, fmt.Errorf("problem creating MD server: %v", err)
	}
	config.SetMDServer(mdServer)

	// note: the mdserver is the keyserver at the moment.
	keyServer, err := makeKeyServer(
		config, params.ServerInMemory, params.ServerRootDir, params.MDServerAddr)
	if err != nil {
		return nil, fmt.Errorf("problem creating key server: %v", err)
	}

	if registry := config.MetricsRegistry(); registry != nil {
		keyServer = NewKeyServerMeasured(keyServer, registry)
	}

	config.SetKeyServer(keyServer)

	daemon, err := makeKeybaseDaemon(config, params.ServerInMemory, params.ServerRootDir, localUser, config.Codec(), config.MakeLogger(""), params.Debug)
	if err != nil {
		return nil, fmt.Errorf("problem creating daemon: %s", err)
	}

	if registry := config.MetricsRegistry(); registry != nil {
		daemon = NewKeybaseDaemonMeasured(daemon, registry)
	}

	config.SetKeybaseDaemon(daemon)

	k := NewKBPKIClient(config)
	config.SetKBPKI(k)

	config.SetReporter(NewReporterKBPKI(config, 10, 1000))

	if localUser == "" {
		c := NewCryptoClient(config, libkb.G)
		config.SetCrypto(c)
	} else {
		signingKey := MakeLocalUserSigningKeyOrBust(localUser)
		cryptPrivateKey := MakeLocalUserCryptPrivateKeyOrBust(localUser)
		config.SetCrypto(NewCryptoLocal(config, signingKey, cryptPrivateKey))
	}

	bserv, err := makeBlockServer(config, params.ServerInMemory, params.ServerRootDir, params.BServerAddr, log)
	if err != nil {
		return nil, fmt.Errorf("cannot open block database: %v", err)
	}

	if registry := config.MetricsRegistry(); registry != nil {
		bserv = NewBlockServerMeasured(bserv, registry)
	}

	config.SetBlockServer(bserv)

	return config, nil
}

// Shutdown does any necessary shutdown tasks for libkbfs. Shutdown
// should be called at the end of main.
func Shutdown() {
	pprof.StopCPUProfile()
}
