package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/go-skynet/LocalAI/core/backend"
	"github.com/go-skynet/LocalAI/core/config"
	"github.com/go-skynet/LocalAI/internal"
	"github.com/go-skynet/LocalAI/pkg/gallery"
	model "github.com/go-skynet/LocalAI/pkg/model"
	progressbar "github.com/schollz/progressbar/v3"

	"github.com/go-skynet/LocalAI/core/http"
	"github.com/go-skynet/LocalAI/core/startup"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	_ "github.com/go-skynet/LocalAI/swagger"
)

type Context struct {
	Debug    bool
	LogLevel string

	Galleries  string
	ModelsPath string

	ImagePath         string
	AudioPath         string
	UploadPath        string
	BackendAssetsPath string
	ConfigPath        string
	LocalaiConfigDir  string
	// TODO: Figure out what ConfigFile is used for
	ModelsConfigFile string
}

type ModelsArgs struct {
	ModelArgs []string `arg:"" optional:"" name:"models" help:"Model configuration URLs to load"`
}

type RunCMD struct {
	ModelsArgs ModelsArgs `embed:""`

	F16         bool `name:"f16" env:"LOCALAI_F16,F16" help:"Enable GPU acceleration" group:"performance tuning"`
	Threads     int  `env:"LOCALAI_THREADS,THREADS" default:"4" help:"Number of threads used for parallel computation. Usage of the number of physical cores in the system is suggested" group:"performance tuning"`
	ContextSize int  `env:"LOCALAI_CONTEXT_SIZE,CONTEXT_SIZE" default:"512" help:"Default context size for models" group:"performance tuning"`

	AutoloadGalleries   bool     `env:"LOCALAI_AUTOLOAD_GALLERIES,AUTOLOAD_GALLERIES" group:"models"`
	RemoteLibrary       string   `env:"LOCALAI_REMOTE_LIBRARY,REMOTE_LIBRARY" default:"${remoteLibraryURL}" help:"A LocalAI remote library URL" group:"models"`
	PreloadModels       string   `env:"LOCALAI_PRELOAD_MODELS,PRELOAD_MODELS" help:"A List of models to apply in JSON at start" group:"models"`
	Models              []string `env:"LOCALAI_MODELS,MODELS" help:"A List of model configuration URLs to load" group:"models"`
	PreloadModelsConfig string   `env:"LOCALAI_PRELOAD_MODELS_CONFIG,PRELOAD_MODELS_CONFIG" help:"A List of models to apply at startup. Path to a YAML config file" group:"models"`

	Address          string   `env:"LOCALAI_ADDRESS,ADDRESS" default:":8080" help:"Bind address for the API server" group:"api"`
	CORS             bool     `env:"LOCALAI_CORS,CORS" help:"" group:"api"`
	CORSAllowOrigins string   `env:"LOCALAI_CORS_ALLOW_ORIGINS,CORS_ALLOW_ORIGINS" group:"api"`
	UploadLimit      int      `env:"LOCALAI_UPLOAD_LIMIT,UPLOAD_LIMIT" default:"15" help:"Default upload-limit in MB" group:"api"`
	APIKeys          []string `env:"LOCALAI_API_KEY,API_KEY" help:"List of API Keys to enable API authentication. When this is set, all the requests must be authenticated with one of these API keys" group:"api"`
	DisableWelcome   bool     `env:"LOCALAI_DISABLE_WELCOME,DISABLE_WELCOME" default:"false" help:"Disable welcome pages" group:"api"`

	ParallelRequests     bool     `env:"LOCALAI_PARALLEL_REQUESTS,PARALLEL_REQUESTS" help:"Enable backends to handle multiple requests in parallel if they support it (e.g.: llama.cpp or vllm)" group:"backends"`
	SingleActiveBackend  bool     `env:"LOCALAI_SINGLE_ACTIVE_BACKEND,SINGLE_ACTIVE_BACKEND" help:"Allow only one backend to be run at a time" group:"backends"`
	PreloadBackendOnly   bool     `env:"LOCALAI_PRELOAD_BACKEND_ONLY,PRELOAD_BACKEND_ONLY" default:"false" help:"Do not launch the API services, only the preloaded models / backends are started (useful for multi-node setups)" group:"backends"`
	ExternalGRPCBackends []string `env:"LOCALAI_EXTERNAL_GRPC_BACKENDS,EXTERNAL_GRPC_BACKENDS" help:"A list of external grpc backends" group:"backends"`
	EnableWatchdogIdle   bool     `env:"LOCALAI_WATCHDOG_IDLE,WATCHDOG_IDLE" default:"false" help:"Enable watchdog for stopping backends that are idle longer than the watchdog-idle-timeout" group:"backends"`
	WatchdogIdleTimeout  string   `env:"LOCALAI_WATCHDOG_IDLE_TIMEOUT,WATCHDOG_IDLE_TIMEOUT" default:"15m" help:"Threshold beyond which an idle backend should be stopped" group:"backends"`
	EnableWatchdogBusy   bool     `env:"LOCALAI_WATCHDOG_BUSY,WATCHDOG_BUSY" default:"false" help:"Enable watchdog for stopping backends that are busy longer than the watchdog-busy-timeout" group:"backends"`
	WatchdogBusyTimeout  string   `env:"LOCALAI_WATCHDOG_BUSY_TIMEOUT,WATCHDOG_BUSY_TIMEOUT" default:"5m" help:"Threshold beyond which a busy backend should be stopped" group:"backends"`
}

func (r *RunCMD) Run(ctx *Context) error {
	log.Debug().Interface("args", r.ModelsArgs.ModelArgs).Msg("In run command")
	opts := []config.AppOption{
		config.WithConfigFile(ctx.ModelsConfigFile),
		config.WithJSONStringPreload(r.PreloadModels),
		config.WithYAMLConfigPreload(r.PreloadModelsConfig),
		config.WithModelPath(ctx.ModelsPath),
		config.WithContextSize(r.ContextSize),
		config.WithDebug(ctx.Debug),
		config.WithImageDir(ctx.ImagePath),
		config.WithAudioDir(ctx.AudioPath),
		config.WithUploadDir(ctx.UploadPath),
		config.WithConfigsDir(ctx.ConfigPath),
		config.WithF16(r.F16),
		config.WithStringGalleries(ctx.Galleries),
		config.WithModelLibraryURL(r.RemoteLibrary),
		config.WithDisableMessage(false),
		config.WithCors(r.CORS),
		config.WithCorsAllowOrigins(r.CORSAllowOrigins),
		config.WithThreads(r.Threads),
		config.WithBackendAssets(backendAssets),
		config.WithBackendAssetsOutput(ctx.BackendAssetsPath),
		config.WithUploadLimitMB(r.UploadLimit),
		config.WithApiKeys(r.APIKeys),
		config.WithModelsURL(append(r.Models, r.ModelsArgs.ModelArgs...)...),
	}

	idleWatchDog := r.EnableWatchdogIdle
	busyWatchDog := r.EnableWatchdogBusy

	if r.DisableWelcome {
		opts = append(opts, config.DisableWelcomePage)
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

	externalgRPC := r.ExternalGRPCBackends
	// split ":" to get backend name and the uri
	for _, v := range externalgRPC {
		backend := v[:strings.IndexByte(v, ':')]
		uri := v[strings.IndexByte(v, ':')+1:]
		opts = append(opts, config.WithExternalBackend(backend, uri))
	}

	if r.AutoloadGalleries {
		opts = append(opts, config.EnableGalleriesAutoload)
	}

	if r.PreloadBackendOnly {
		_, _, _, err := startup.Startup(opts...)
		return err
	}

	cl, ml, options, err := startup.Startup(opts...)

	if err != nil {
		return fmt.Errorf("failed basic startup tasks with error %s", err.Error())
	}

	configdir := ctx.LocalaiConfigDir
	// Watch the configuration directory
	// If the directory does not exist, we don't watch it
	if _, err := os.Stat(configdir); err == nil {
		closeConfigWatcherFn, err := startup.WatchConfigDirectory(ctx.LocalaiConfigDir, options)
		defer closeConfigWatcherFn()

		if err != nil {
			return fmt.Errorf("failed while watching configuration directory %s", ctx.LocalaiConfigDir)
		}
	}

	appHTTP, err := http.App(cl, ml, options)
	if err != nil {
		log.Error().Err(err).Msg("error during HTTP App construction")
		return err
	}

	return appHTTP.Listen(r.Address)
}

type ModelsList struct{}
type ModelsInstall struct {
	ModelsArgs ModelsArgs `embed:""`
}
type ModelsCMD struct {
	List    ModelsList    `cmd:"" help:"List the models avaiable in your galleries"`
	Install ModelsInstall `cmd:"" help:"Install a model from the gallery"`
}

func (ml *ModelsList) Run(ctx *Context) error {
	log.Debug().Msg("In models list command")
	var galleries []gallery.Gallery
	if err := json.Unmarshal([]byte(ctx.Galleries), &galleries); err != nil {
		log.Error().Err(err).Msg("unable to load galleries")
	}

	models, err := gallery.AvailableGalleryModels(galleries, ctx.ModelsPath)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.Installed {
			fmt.Printf(" * %s@%s (installed)\n", model.Gallery.Name, model.Name)
		} else {
			fmt.Printf(" - %s@%s\n", model.Gallery.Name, model.Name)
		}
	}
	return nil
}

func (mi *ModelsInstall) Run(ctx *Context) error {
	log.Debug().Msg("In models install command")
	modelName := mi.ModelsArgs.ModelArgs[0]

	var galleries []gallery.Gallery
	if err := json.Unmarshal([]byte(ctx.Galleries), &galleries); err != nil {
		log.Error().Err(err).Msg("unable to load galleries")
	}

	progressBar := progressbar.NewOptions(
		1000,
		progressbar.OptionSetDescription(fmt.Sprintf("downloading model %s", modelName)),
		progressbar.OptionShowBytes(false),
		progressbar.OptionClearOnFinish(),
	)
	progressCallback := func(fileName string, current string, total string, percentage float64) {
		progressBar.Set(int(percentage * 10))
	}
	err := gallery.InstallModelFromGallery(galleries, modelName, ctx.ModelsPath, gallery.GalleryModel{}, progressCallback)
	if err != nil {
		return err
	}
	return nil
}

type TTSCMD struct {
	Text []string `arg:""`

	Backend    string `short:"b" default:"piper" help:"Backend to run the TTS model"`
	Model      string `short:"m" required:"" help:"Model name to run the TTS"`
	Voice      string `short:"v" help:"Voice name to run the TTS"`
	OutputFile string `short:"o" type:"path" help:"The path to write the output wav file"`
}

func (t *TTSCMD) Run(ctx *Context) error {
	log.Debug().Msg("In tts command")

	outputFile := t.OutputFile
	outputDir := ctx.BackendAssetsPath
	if outputFile != "" {
		outputDir = filepath.Dir(outputFile)
	}

	text := strings.Join(t.Text, " ")

	opts := &config.ApplicationConfig{
		ModelPath:         ctx.ModelsPath,
		Context:           context.Background(),
		AudioDir:          outputDir,
		AssetsDestination: ctx.BackendAssetsPath,
	}
	ml := model.NewModelLoader(opts.ModelPath)

	defer ml.StopAllGRPC()

	options := config.BackendConfig{}
	options.SetDefaults()

	filePath, _, err := backend.ModelTTS(t.Backend, text, t.Model, t.Voice, ml, opts, options)
	if err != nil {
		return err
	}
	if outputFile != "" {
		if err := os.Rename(filePath, outputFile); err != nil {
			return err
		}
		fmt.Printf("Generate file %s\n", outputFile)
	} else {
		fmt.Printf("Generate file %s\n", filePath)
	}
	return nil
}

type TranscriptCMD struct {
	Filename string `arg:""`

	Backend  string `short:"b" default:"whisper" help:"Backend to run the transcription model"`
	Model    string `short:"m" required:"" help:"Model name to run the TTS"`
	Language string `short:"l" help:"Language of the audio file"`
	Threads  int    `short:"t" default:"1" help:"Number of threads used for parallel computation"`
}

func (t *TranscriptCMD) Run(ctx *Context) error {
	log.Debug().Msg("In transcript command")

	opts := &config.ApplicationConfig{
		ModelPath:         ctx.ModelsPath,
		Context:           context.Background(),
		AssetsDestination: ctx.BackendAssetsPath,
	}

	cl := config.NewBackendConfigLoader()
	ml := model.NewModelLoader(opts.ModelPath)
	if err := cl.LoadBackendConfigsFromPath(ctx.ModelsPath); err != nil {
		return err
	}

	c, exists := cl.GetBackendConfig(t.Model)
	if !exists {
		return errors.New("model not found")
	}

	c.Threads = &t.Threads

	defer ml.StopAllGRPC()

	tr, err := backend.ModelTranscription(t.Filename, t.Language, ml, c, opts)
	if err != nil {
		return err
	}
	for _, segment := range tr.Segments {
		fmt.Println(segment.Start.String(), "-", segment.Text)
	}
	return nil
}

var cli struct {
	Debug      bool    `env:"LOCALAI_DEBUG,DEBUG" default:"false" help:"DEPRECATED, use --log-level=debug instead. Enable debug logging"`
	LogLevel   *string `env:"LOCALAI_LOG_LEVEL" enum:"error,warn,info,debug" help:"Set the level of logs to output"`
	Galleries  string  `env:"LOCALAI_GALLERIES,GALLERIES" help:"JSON list of galleries" group:"models"`
	ModelsPath string  `env:"LOCALAI_MODELS_PATH,MODELS_PATH" type:"path" default:"${basepath}/models" help:"Path containing models used for inferencing" group:"storage"`

	ImagePath         string `env:"LOCALAI_IMAGE_PATH,IMAGE_PATH" type:"path" default:"/tmp/generated/images" help:"Location for images generated by backends (e.g. stablediffusion)" group:"storage"`
	AudioPath         string `env:"LOCALAI_AUDIO_PATH,AUDIO_PATH" type:"path" default:"/tmp/generated/audio" help:"Location for audio generated by backends (e.g. piper)" group:"storage"`
	UploadPath        string `env:"LOCALAI_UPLOAD_PATH,UPLOAD_PATH" type:"path" default:"/tmp/localai/upload" help:"Path to store uploads from files api" group:"storage"`
	BackendAssetsPath string `env:"LOCALAI_BACKEND_ASSETS_PATH,BACKEND_ASSETS_PATH" type:"path" default:"/tmp/localai/backend_data" help:"Path used to extract libraries that are required by some of the backends in runtime" group:"storage"`
	ConfigPath        string `env:"LOCALAI_CONFIG_PATH,CONFIG_PATH" default:"/tmp/localai/config" group:"storage"`
	LocalaiConfigDir  string `env:"LOCALAI_CONFIG_DIR" type:"path" default:"${basepath}/configuration" help:"Directory for dynamic loading of certain configuration files (currently api_keys.json and external_backends.json)" group:"storage"`
	// The alias on this option is there to preserve functionality with the old `--config-file` parameter
	ModelsConfigFile string `env:"LOCALAI_MODELS_CONFIG_FILE,CONFIG_FILE" aliases:"config-file" help:"YAML file containing a list of model backend configs" group:"storage"`

	Run        RunCMD        `cmd:"" help:"Run LocalAI, this the default command if no other command is specified" default:"withargs"`
	Models     ModelsCMD     `cmd:"" help:"Manage LocalAI models and definitions"`
	TTS        TTSCMD        `cmd:"" help:"Convert text to speech"`
	Transcript TranscriptCMD `cmd:"" help:"Convert audio to text"`
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	// clean up process
	go func() {
		c := make(chan os.Signal, 1) // we need to reserve to buffer size 1, so the notifier are not blocked
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		os.Exit(1)
	}()

	ctx := kong.Parse(&cli,
		kong.Configuration(kongyaml.Loader, "./localai.yaml", "~/.config/localai.yaml", "/etc/localai.yaml"),
		kong.Description(
			`  LocalAI is a drop-in replacement OpenAI API for running LLM, GPT and genAI models locally on CPU, GPUs with consumer grade hardware.

Some of the models compatible are:
  - Vicuna
  - Koala
  - GPT4ALL
  - GPT4ALL-J
  - Cerebras
  - Alpaca
  - StableLM (ggml quantized)

For a list of compatible models, check out: https://localai.io/model-compatibility/index.html

Copyright: Ettore Di Giacinto

Version: ${version}
`,
		),
		kong.UsageOnError(),
		kong.Vars{
			"basepath":         kong.ExpandPath("."),
			"remoteLibraryURL": "https://raw.githubusercontent.com/mudler/LocalAI/master/embedded/model_library.yaml",
			"version":          internal.PrintableVersion(),
		},
	)

	// This is here to preserve the existing --debug flag functionality
	logLevel := "info"
	if cli.Debug && cli.LogLevel == nil {
		logLevel = "debug"
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		cli.LogLevel = &logLevel
	}

	if cli.LogLevel == nil {
		cli.LogLevel = &logLevel
	}

	log.Info().Str("loglevel", *cli.LogLevel).Msg("Current log level")

	switch *cli.LogLevel {
	case "error":
		log.Info().Msg("Setting logging to error")
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "warn":
		log.Info().Msg("Setting logging to warn")
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "info":
		log.Info().Msg("Setting logging to info")
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "debug":
		log.Info().Msg("Setting logging to debug")
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	err := ctx.Run(&Context{
		Debug:      *cli.LogLevel == "debug",
		LogLevel:   *cli.LogLevel,
		Galleries:  cli.Galleries,
		ModelsPath: cli.ModelsPath,

		ImagePath:         cli.ImagePath,
		AudioPath:         cli.AudioPath,
		UploadPath:        cli.UploadPath,
		BackendAssetsPath: cli.BackendAssetsPath,
		ConfigPath:        cli.ConfigPath,
		LocalaiConfigDir:  cli.LocalaiConfigDir,
		// TODO: Figure out what ConfigFile is used for
		ModelsConfigFile: cli.ModelsConfigFile,
	})
	ctx.FatalIfErrorf(err)
}
