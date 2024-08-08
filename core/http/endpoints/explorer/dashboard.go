package explorer

import (
	"sort"

	"github.com/gofiber/fiber/v2"
	"github.com/mudler/LocalAI/core/explorer"
	"github.com/mudler/LocalAI/internal"
)

func Dashboard() func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {

		summary := fiber.Map{
			"Title":   "LocalAI API - " + internal.PrintableVersion(),
			"Version": internal.PrintableVersion(),
		}

		if string(c.Context().Request.Header.ContentType()) == "application/json" || len(c.Accepts("html")) == 0 {
			// The client expects a JSON response
			return c.Status(fiber.StatusOK).JSON(summary)
		} else {
			// Render index
			return c.Render("views/explorer", summary)
		}
	}
}

type AddNetworkRequest struct {
	Token       string `json:"token"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Network struct {
	explorer.Network
	explorer.TokenData
	Token string `json:"token"`
}

func ShowNetworks(db *explorer.Database, ds *explorer.DiscoveryServer) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		networkState := ds.NetworkState()
		results := []Network{}
		for token, network := range networkState.Networks {
			networkData, exists := db.Get(token) // get the token data
			if exists {
				results = append(results, Network{Network: network, TokenData: networkData, Token: token})
			}
		}

		// order by number of clusters
		sort.Slice(results, func(i, j int) bool {
			return len(results[i].Clusters) > len(results[j].Clusters)
		})

		return c.JSON(results)
	}
}

func AddNetwork(db *explorer.Database) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		request := new(AddNetworkRequest)
		if err := c.BodyParser(request); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cannot parse JSON"})
		}

		if request.Token == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token is required"})
		}

		if request.Name == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
		}

		if request.Description == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Description is required"})
		}

		// TODO: check if token is valid, otherwise reject

		err := db.Set(request.Token, explorer.TokenData{Name: request.Name, Description: request.Description})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Cannot add token"})
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Token added"})
	}
}
