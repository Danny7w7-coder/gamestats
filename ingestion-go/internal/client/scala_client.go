// Package client envía las partidas terminadas al servicio de Scala.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"gamestats/ingestion/internal/model"
)

// ScalaClient hace POST {SCALA_URL}/analyze con reintentos.
type ScalaClient struct {
	url        string
	httpClient *http.Client
	// backoff son las esperas antes de cada reintento: 1 s, 2 s y 4 s.
	// La cantidad de esperas es la cantidad de reintentos (3).
	backoff []time.Duration
}

// New crea un cliente para el Scala que está en baseURL.
func New(baseURL string) *ScalaClient {
	return &ScalaClient{
		url: strings.TrimRight(baseURL, "/") + "/analyze",
		// Sin timeout, un Scala colgado dejaría al sender esperando para siempre.
		httpClient: &http.Client{Timeout: 10 * time.Second},
		backoff:    []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second},
	}
}

// Send envía el lote. Si falla por red, timeout o un 5xx, reintenta hasta 3
// veces esperando cada vez más. Un 4xx no se reintenta: significa que el lote
// está mal y mandarlo de nuevo no lo arregla. Devuelve nil si Scala lo aceptó.
func (c *ScalaClient) Send(b model.Batch) error {
	body, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("no se pudo convertir el lote a JSON: %w", err)
	}

	retry, err := c.post(body)
	for i, wait := range c.backoff {
		if err == nil || !retry {
			break
		}
		log.Printf("envío de %s falló (%v); reintento %d de %d en %s",
			b.MatchID, err, i+1, len(c.backoff), wait)
		time.Sleep(wait)
		retry, err = c.post(body)
	}
	return err
}

// post hace un solo intento. retry indica si vale la pena reintentar.
func (c *ScalaClient) post(body []byte) (retry bool, err error) {
	resp, err := c.httpClient.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return true, err // Scala caído o timeout
	}
	defer resp.Body.Close()
	// Se lee la respuesta completa para que la conexión se pueda reutilizar.
	io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return false, fmt.Errorf("Scala rechazó el lote: %s", resp.Status)
	default:
		return true, fmt.Errorf("Scala respondió %s", resp.Status)
	}
}
