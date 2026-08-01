// SPDX-Licence-Identifier: EUPL-1.2

package api

import (
	"net/http"

	"dappco.re/go/i18n/reversal"
	html "dappco.re/go/render/engine/html"
	"github.com/gin-gonic/gin"
)

const todoRenderDataBinding = "TODO(#731): implement go-html template/data render primitives for non-Go consumers"

type renderRequest struct {
	Template string         `json:"template"`
	Data     map[string]any `json:"data,omitempty"`
	Locale   string         `json:"locale,omitempty"`
}

type renderResponse struct {
	HTML string `json:"html"`
}

type grammarCheckRequest struct {
	HTML          string                   `json:"html"`
	Locale        string                   `json:"locale,omitempty"`
	Reference     *reversal.GrammarImprint `json:"reference,omitempty"`
	MinSimilarity float64                  `json:"min_similarity,omitempty"`
}

type grammarCheckResponse struct {
	Valid      bool                    `json:"valid"`
	Imprint    reversal.GrammarImprint `json:"imprint"`
	Similarity *float64                `json:"similarity,omitempty"`
	TokenCount int                     `json:"token_count"`
}

func (p *HTMLProvider) render(c *gin.Context) {
	if c == nil {
		return
	}

	var req renderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request body"})
		return
	}
	if req.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template is required"})
		return
	}

	if len(req.Data) > 0 {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "template data binding is not implemented",
			"todo":  todoRenderDataBinding,
		})
		return
	}

	ctx := html.NewContext()
	if req.Locale != "" {
		ctx.SetLocale(req.Locale)
	}
	c.JSON(http.StatusOK, renderResponse{HTML: html.Render(html.Raw(req.Template), ctx)})
}

func (p *HTMLProvider) checkGrammar(c *gin.Context) {
	if c == nil {
		return
	}

	var req grammarCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request body"})
		return
	}
	if req.HTML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "html is required"})
		return
	}

	ctx := html.NewContext()
	if req.Locale != "" {
		ctx.SetLocale(req.Locale)
	}

	imprint := html.Imprint(html.Raw(req.HTML), ctx)
	resp := grammarCheckResponse{
		Valid:      true,
		Imprint:    imprint,
		TokenCount: imprint.TokenCount,
	}

	if req.Reference != nil {
		threshold := req.MinSimilarity
		if threshold == 0 {
			threshold = 0.8
		}
		similarity := imprint.Similar(*req.Reference)
		resp.Similarity = &similarity
		resp.Valid = similarity >= threshold
	}

	c.JSON(http.StatusOK, resp)
}
