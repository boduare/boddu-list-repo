package transformers

// This is a wrapper to statisfy the GRPC service interface
// It is meant to be used by the main executable that is the server for the specific backend type (falcon, gpt3, etc)
import (
	"fmt"

	"github.com/go-skynet/LocalAI/pkg/grpc/base"
	pb "github.com/go-skynet/LocalAI/pkg/grpc/proto"
	"github.com/rs/zerolog/log"

	transformers "github.com/go-skynet/go-ggml-transformers.cpp"
)

type Dolly struct {
	base.Base

	dolly *transformers.Dolly
}

func (llm *Dolly) Load(opts *pb.ModelOptions) error {
	if llm.Base.State != pb.StateResponse_UNINITIALIZED {
		log.Warn().Msgf("dolly backend loading %s while already in state %s!", opts.Model, llm.Base.State.String())
	}

	llm.Base.Lock()
	defer llm.Base.Unlock()
	model, err := transformers.NewDolly(opts.ModelFile)
	llm.dolly = model
	return err
}

func (llm *Dolly) Predict(opts *pb.PredictOptions) (string, error) {
	llm.Base.Lock()
	defer llm.Base.Unlock()
	return llm.dolly.Predict(opts.Prompt, buildPredictOptions(opts)...)
}

// fallback to Predict
func (llm *Dolly) PredictStream(opts *pb.PredictOptions, results chan string) error {
	llm.Base.Lock()

	go func() {
		res, err := llm.dolly.Predict(opts.Prompt, buildPredictOptions(opts)...)

		if err != nil {
			fmt.Println("err: ", err)
		}
		results <- res
		close(results)
		llm.Base.Unlock()
	}()

	return nil
}
