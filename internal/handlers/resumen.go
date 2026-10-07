package handlers

import (
	"net/http"

	"finanzas-api/internal/auth"
	"finanzas-api/internal/httpx"
)

type totalCategoria struct {
	Categoria string  `json:"categoria"`
	Total     float64 `json:"total"`
}

type Resumen struct {
	Mes                string           `json:"mes"`
	TotalIngresos      float64          `json:"total_ingresos"`
	TotalGastos        float64          `json:"total_gastos"`
	Balance            float64          `json:"balance"`
	GastosPorCategoria []totalCategoria `json:"gastos_por_categoria"`
}

// GetResumen devuelve ingresos, gastos y balance del mes (?mes=AAAA-MM).
func (a *API) GetResumen(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	primero, ok := a.primerDiaMes(w, r)
	if !ok {
		return
	}
	siguiente := primero.AddDate(0, 1, 0)

	res := Resumen{Mes: primero.Format("2006-01"), GastosPorCategoria: []totalCategoria{}}

	err := a.DB.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(monto), 0)::float8 FROM ingresos WHERE usuario_id = $1 AND fecha >= $2 AND fecha < $3`,
		uid, primero, siguiente).Scan(&res.TotalIngresos)
	if err != nil {
		httpx.DBError(w, err)
		return
	}

	rows, err := a.DB.Query(r.Context(), `
		SELECT COALESCE(c.nombre, 'Sin categoría'), SUM(g.monto)::float8
		FROM gastos g
		LEFT JOIN categorias c ON c.id = g.categoria_id
		WHERE g.usuario_id = $1 AND g.fecha >= $2 AND g.fecha < $3
		GROUP BY 1
		ORDER BY 2 DESC`, uid, primero, siguiente)
	if err != nil {
		httpx.DBError(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t totalCategoria
		if err := rows.Scan(&t.Categoria, &t.Total); err != nil {
			httpx.DBError(w, err)
			return
		}
		res.TotalGastos += t.Total
		res.GastosPorCategoria = append(res.GastosPorCategoria, t)
	}
	if err := rows.Err(); err != nil {
		httpx.DBError(w, err)
		return
	}
	res.Balance = res.TotalIngresos - res.TotalGastos
	httpx.JSON(w, http.StatusOK, res)
}
