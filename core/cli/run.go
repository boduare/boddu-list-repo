package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	cliContext "github.com/go-skynet/LocalAI/core/cli/context"
	"github.com/go-skynet/LocalAI/core/config"
	"github.com/go-skynet/LocalAI/core/http"
	"github.com/go-skynet/LocalAI/core/p2p"
	"github.com/go-skynet/LocalAI/core/startup"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type RunCMD struct {
	ModelArgs []string `arg:"" optional:"" name:"models" help:"Model configuration URLs to load"`

	ModelsPath                   string        `env:"LOCALAI_MODELS_PATH,MODELS_PATH" type:"path" default:"${basepath}/models" help:"Path containing models used for inferencing" group:"storage"`
	BackendAssetsPath            string        `env:"LOCALAI_BACKEND_ASSETS_PATH,BACKEND_ASSETS_PATH" type:"path" default:"/tmp/localai/backend_data" help:"Path used to extract libraries that are required by some of the backends in runtime" group:"storage"`
	ImagePath                    string        `env:"LOCALAI_IMAGE_PATH,IMAGE_PATH" type:"path" default:"/tmp/generated/images" help:"Location for images generated by backends (e.g. stablediffusion)" group:"storage"`
	AudioPath                    string        `env:"LOCALAI_AUDIO_PATH,AUDIO_PATH" type:"path" default:"/tmp/generated/audio" help:"Location for audio generated by backends (e.g. piper)" group:"storage"`
	UploadPath                   string        `env:"LOCALAI_UPLOAD_PATH,UPLOAD_PATH" type:"path" default:"/tmp/localai/upload" help:"Path to store uploads from files api" group:"storage"`
	ConfigPath                   string        `env:"LOCALAI_CONFIG_PATH,CONFIG_PATH" default:"/tmp/localai/config" group:"storage"`
	LocalaiConfigDir             string        `env:"LOCALAI_CONFIG_DIR" type:"path" default:"${basepath}/configuration" help:"Directory for dynamic loading of certain configuration files (currently api_keys.json and external_backends.json)" group:"storage"`
	LocalaiConfigDirPollInterval time.Duration `env:"LOCALAI_CONFIG_DIR_POLL_INTERVAL" help:"Typically the config path picks up changes automatically, but if your system has broken fsnotify events, set this to an interval to poll the LocalAI Config Dir (example: 1m)" group:"storage"`
	// The alias on this option is there to preserve functionality with the old `--config-file` parameter
	ModelsConfigFile string `env:"LOCALAI_MODELS_CONFIG_FILE,CONFIG_FILE" aliases:"config-file" help:"YAML file containing a list of model backend configs" group:"storage"`

	Galleries           string   `env:"LOCALAI_GALLERIES,GALLERIES" help:"JSON list of galleries" group:"models" default:"${galleries}"`
	AutoloadGalleries   bool     `env:"LOCALAI_AUTOLOAD_GALLERIES,AUTOLOAD_GALLERIES" group:"models"`
	RemoteLibrary       string   `env:"LOCALAI_REMOTE_LIBRARY,REMOTE_LIBRARY" default:"${remoteLibraryURL}" help:"A LocalAI remote library URL" group:"models"`
	PreloadModels       string   `env:"LOCALAI_PRELOAD_MODELS,PRELOAD_MODELS" help:"A List of models to apply in JSON at start" group:"models"`
	Models              []string `env:"LOCALAI_MODELS,MODELS" help:"A List of model configuration URLs to load" group:"models"`
	PreloadModelsConfig string   `env:"LOCALAI_PRELOAD_MODELS_CONFIG,PRELOAD_MODELS_CONFIG" help:"A List of models to apply at startup. Path to a YAML config file" group:"models"`

	F16         bool `name:"f16" env:"LOCALAI_F16,F16" help:"Enable GPU acceleration" group:"performance"`
	Threads     int  `env:"LOCALAI_THREADS,THREADS" short:"t" help:"Number of threads used for parallel computation. Usage of the number of physical cores in the system is suggested" group:"performance"`
	ContextSize int  `env:"LOCALAI_CONTEXT_SIZE,CONTEXT_SIZE" default:"512" help:"Default context size for models" group:"performance"`

	Address              string   `env:"LOCALAI_ADDRESS,ADDRESS" default:":8080" help:"Bind address for the API server" group:"api"`
	CORS                 bool     `env:"LOCALAI_CORS,CORS" help:"" group:"api"`
	CORSAllowOrigins     string   `env:"LOCALAI_CORS_ALLOW_ORIGINS,CORS_ALLOW_ORIGINS" group:"api"`
	CSRF                 bool     `env:"LOCALAI_CSRF" help:"Enables fiber CSRF middleware" group:"api"`
	UploadLimit          int      `env:"LOCALAI_UPLOAD_LIMIT,UPLOAD_LIMIT" default:"15" help:"Default upload-limit in MB" group:"api"`
	APIKeys              []string `env:"LOCALAI_API_KEY,API_KEY" help:"List of API Keys to enable API authentication. When this is set, all the requests must be authenticated with one of these API keys" group:"api"`
	DisableWebUI         bool     `env:"LOCALAI_DISABLE_WEBUI,DISABLE_WEBUI" default:"false" help:"Disable webui" group:"api"`
	OpaqueErrors         bool     `env:"LOCALAI_OPAQUE_ERRORS" default:"false" help:"If true, all error responses are replaced with blank 500 errors. This is intended only for hardening against information leaks and is normally not recommended." group:"api"`
	Peer2Peer            bool     `env:"LOCALAI_P2P,P2P" name:"p2p" default:"false" help:"Enable P2P mode" group:"p2p"`
	Peer2PeerToken       string   `env:"LOCALAI_P2P_TOKEN,P2P_TOKEN" name:"p2ptoken" help:"Token for P2P mode (optional)" group:"p2p"`
	ParallelRequests     bool     `env:"LOCALAI_PARALLEL_REQUESTS,PARALLEL_REQUESTS" help:"Enable backends to handle multiple requests in parallel if they support it (e.g.: llama.cpp or vllm)" group:"backends"`
	SingleActiveBackend  bool     `env:"LOCALAI_SINGLE_ACTIVE_BACKEND,SINGLE_ACTIVE_BACKEND" help:"Allow only one backend to be run at a time" group:"backends"`
	PreloadBackendOnly   bool     `env:"LOCALAI_PRELOAD_BACKEND_ONLY,PRELOAD_BACKEND_ONLY" default:"false" help:"Do not launch the API services, only the preloaded models / backends are started (useful for multi-node setups)" group:"backends"`
	ExternalGRPCBackends []string `env:"LOCALAI_EXTERNAL_GRPC_BACKENDS,EXTERNAL_GRPC_BACKENDS" help:"A list of external grpc backends" group:"backends"`
	EnableWatchdogIdle   bool     `env:"LOCALAI_WATCHDOG_IDLE,WATCHDOG_IDLE" default:"false" help:"Enable watchdog for stopping backends that are idle longer than the watchdog-idle-timeout" group:"backends"`
	WatchdogIdleTimeout  string   `env:"LOCALAI_WATCHDOG_IDLE_TIMEOUT,WATCHDOG_IDLE_TIMEOUT" default:"15m" help:"Threshold beyond which an idle backend should be stopped" group:"backends"`
	EnableWatchdogBusy   bool     `env:"LOCALAI_WATCHDOG_BUSY,WATCHDOG_BUSY" default:"false" help:"Enable watchdog for stopping backends that are busy longer than the watchdog-busy-timeout" group:"backends"`
	WatchdogBusyTimeout  string   `env:"LOCALAI_WATCHDOG_BUSY_TIMEOUT,WATCHDOG_BUSY_TIMEOUT" default:"5m" help:"Threshold beyond which a busy backend should be stopped" group:"backends"`
}

func (r *RunCMD) Run(ctx *cliContext.Context) error {
	opts := []config.AppOption{
		config.WithConfigFile(r.ModelsConfigFile),
		config.WithJSONStringPreload(r.PreloadModels),
		config.WithYAMLConfigPreload(r.PreloadModelsConfig),
		config.WithModelPath(r.ModelsPath),
		config.WithContextSize(r.ContextSize),
		config.WithDebug(zerolog.GlobalLevel() <= zerolog.DebugLevel),
		config.WithImageDir(r.ImagePath),
		config.WithAudioDir(r.AudioPath),
		config.WithUploadDir(r.UploadPath),
		config.WithConfigsDir(r.ConfigPath),
		config.WithDynamicConfigDir(r.LocalaiConfigDir),
		config.WithDynamicConfigDirPollInterval(r.LocalaiConfigDirPollInterval),
		config.WithF16(r.F16),
		config.WithStringGalleries(r.Galleries),
		config.WithModelLibraryURL(r.RemoteLibrary),
		config.WithCors(r.CORS),
		config.WithCorsAllowOrigins(r.CORSAllowOrigins),
		config.WithCsrf(r.CSRF),
		config.WithThreads(r.Threads),
		config.WithBackendAssets(ctx.BackendAssets),
		config.WithBackendAssetsOutput(r.BackendAssetsPath),
		config.WithUploadLimitMB(r.UploadLimit),
		config.WithApiKeys(r.APIKeys),
		config.WithModelsURL(append(r.Models, r.ModelArgs...)...),
		config.WithOpaqueErrors(r.OpaqueErrors),
	}

	if r.Peer2Peer || r.Peer2PeerToken != "" {
		log.Info().Msg("P2P mode enabled")
		token := r.Peer2PeerToken
		if token == "" {
			// IF no token is provided, and p2p is enabled,
			// we generate one and wait for the user to pick up the token (this is for interactive)
			log.Info().Msg("No token provided, generating one")
			token = p2p.GenerateToken()
			log.Info().Msg("Generated Token:")
			fmt.Println(token)

			log.Info().Msg("To use the token, you can run the following command in another node or terminal:")
			fmt.Printf("export TOKEN=\"%s\"\nlocal-ai worker p2p-llama-cpp-rpc\n", token)

			// Ask for user confirmation
			log.Info().Msg("Press a button to proceed")
			var input string
			fmt.Scanln(&input)
		}
		log.Info().Msg("Starting P2P server discovery...")
		if err := p2p.LLamaCPPRPCServerDiscoverer(context.Background(), token); err != nil {
			return err
		}
	}

	idleWatchDog := r.EnableWatchdogIdle
	busyWatchDog := r.EnableWatchdogBusy

	if r.DisableWebUI {
		opts = append(opts, config.DisableWebUI)
	}

	if idleWatchDog || busyWatchDog {
		opts = append(opts, config.EnableWatchDog)
		if idleWatchDog {
			opts = append(opts, config.EnableWatchDogIdleCheck)
			dur, err := time.ParseDuration(r.WatchdogIdleTimeout)
			if err != nil {
				return err
			}
			opts = append(opts, config.SetWatchDogIdleTimeout(dur))
		}
		if busyWatchDog {
			opts = append(opts, config.EnableWatchDogBusyCheck)
			dur, err := time.ParseDuration(r.WatchdogBusyTimeout)
			if err != nil {
				return err
			}
			opts = append(opts, config.SetWatchDogBusyTimeout(dur))
		}
	}
	if r.ParallelRequests {
		opts = append(opts, config.EnableParallelBackendRequests)
	}
	if r.SingleActiveBackend {
		opts = append(opts, config.EnableSingleBackend)
	}

	// split ":" to get backend name and the uri
	for _, v := range r.ExternalGRPCBackends {
		backend := v[:strings.IndexByte(v, ':')]
		uri := v[strings.IndexByte(v, ':')+1:]
		opts = append(opts, config.WithExternalBackend(backend, uri))
	}

	if r.AutoloadGalleries {
		opts = append(opts, config.EnableGalleriesAutoload)
	}

	if r.PreloadBackendOnly {
		_, err := startup.Startup(opts...)
		return err
	}

	app, err := startup.Startup(opts...)
	if err != nil {
		return fmt.Errorf("failed basic startup tasks with error %s", err.Error())
	}

	appHTTP, err := http.App(app)
	if err != nil {
		log.Error().Err(err).Msg("error during HTTP App construction")
		return err
	}

	return appHTTP.Listen(r.Address)
}
