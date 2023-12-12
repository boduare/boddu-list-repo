package localai

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/go-skynet/LocalAI/core/backend"
	"github.com/go-skynet/LocalAI/pkg/gallery"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

/// Endpoint Service

type ModelGalleryService struct {
	galleries      []gallery.Gallery
	modelPath      string
	galleryApplier *backend.GalleryApplier
}

type GalleryModel struct {
	ID string `json:"id"`
	gallery.GalleryModel
}

func CreateModelGalleryService(galleries []gallery.Gallery, modelPath string, galleryApplier *backend.GalleryApplier) ModelGalleryService {
	return ModelGalleryService{
		galleries:      galleries,
		modelPath:      modelPath,
		galleryApplier: galleryApplier,
	}
}

func (mgs *ModelGalleryService) GetOpStatusEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		status := mgs.galleryApplier.GetStatus(c.Params("uuid"))
		if status == nil {
			return fmt.Errorf("could not find any status for ID")
		}
		return c.JSON(status)
	}
}

func (mgs *ModelGalleryService) GetAllStatusEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.JSON(mgs.galleryApplier.GetAllStatus())
	}
}

func (mgs *ModelGalleryService) ApplyModelGalleryEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		input := new(GalleryModel)
		// Get input data from the request body
		if err := c.BodyParser(input); err != nil {
			return err
		}

		uuid, err := uuid.NewUUID()
		if err != nil {
			return err
		}
		mgs.galleryApplier.C <- gallery.GalleryOp{
			Req:         input.GalleryModel,
			Id:          uuid.String(),
			GalleryName: input.ID,
			Galleries:   mgs.galleries,
		}
		return c.JSON(struct {
			ID        string `json:"uuid"`
			StatusURL string `json:"status"`
		}{ID: uuid.String(), StatusURL: c.BaseURL() + "/models/jobs/" + uuid.String()})
	}
}

func (mgs *ModelGalleryService) ListModelFromGalleryEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		log.Debug().Msgf("Listing models from galleries: %+v", mgs.galleries)

		models, err := gallery.AvailableGalleryModels(mgs.galleries, mgs.modelPath)
		if err != nil {
			return err
		}
		log.Debug().Msgf("Models found from galleries: %+v", models)
		for _, m := range models {
			log.Debug().Msgf("Model found from galleries: %+v", m)
		}
		dat, err := json.Marshal(models)
		if err != nil {
			return err
		}
		return c.Send(dat)
	}
}

// NOTE: This is different (and much simpler!) than above! This JUST lists the model galleries that have been loaded, not their contents!
func (mgs *ModelGalleryService) ListModelGalleriesEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		log.Debug().Msgf("Listing model galleries %+v", mgs.galleries)
		dat, err := json.Marshal(mgs.galleries)
		if err != nil {
			return err
		}
		return c.Send(dat)
	}
}

func (mgs *ModelGalleryService) AddModelGalleryEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		input := new(gallery.Gallery)
		// Get input data from the request body
		if err := c.BodyParser(input); err != nil {
			return err
		}
		if slices.ContainsFunc(mgs.galleries, func(gallery gallery.Gallery) bool {
			return gallery.Name == input.Name
		}) {
			return fmt.Errorf("%s already exists", input.Name)
		}
		dat, err := json.Marshal(mgs.galleries)
		if err != nil {
			return err
		}
		log.Debug().Msgf("Adding %+v to gallery list", *input)
		mgs.galleries = append(mgs.galleries, *input)
		return c.Send(dat)
	}
}

func (mgs *ModelGalleryService) RemoveModelGalleryEndpoint() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		input := new(gallery.Gallery)
		// Get input data from the request body
		if err := c.BodyParser(input); err != nil {
			return err
		}
		if !slices.ContainsFunc(mgs.galleries, func(gallery gallery.Gallery) bool {
			return gallery.Name == input.Name
		}) {
			return fmt.Errorf("%s is not currently registered", input.Name)
		}
		mgs.galleries = slices.DeleteFunc(mgs.galleries, func(gallery gallery.Gallery) bool {
			return gallery.Name == input.Name
		})
		return c.Send(nil)
	}
}
