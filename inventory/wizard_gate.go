package inventory

import (
	"context"
	"database/sql"
	"encoding/json"

	appErrs "encore.app/wabantu/shared/errs"
)

func loadWizardSnapshot(ctx context.Context, conn *sql.Conn) (WizardAnswers, WizardRecommendation, error) {
	var answersJSON, recJSON []byte
	err := conn.QueryRowContext(ctx, `
		SELECT wizard_answers, wizard_recommendation
		FROM inv_setting
		ORDER BY created_at
		LIMIT 1`).Scan(&answersJSON, &recJSON)
	if err != nil {
		return WizardAnswers{}, WizardRecommendation{}, err
	}

	var answers WizardAnswers
	if len(answersJSON) > 0 {
		_ = json.Unmarshal(answersJSON, &answers)
	}
	sanitizeWizardAnswers(&answers)

	var rec WizardRecommendation
	if len(recJSON) > 0 {
		_ = json.Unmarshal(recJSON, &rec)
	}
	return answers, rec, nil
}

func wizardInterviewCompleted(answers WizardAnswers, rec WizardRecommendation) bool {
	if !wizardAnswersReady(answers) {
		return false
	}
	_, ok := normalizeCostingMethod(rec.Method)
	return ok
}

func validateInventorySetupActivation(answers WizardAnswers, rec WizardRecommendation) error {
	if !wizardAnswersReady(answers) {
		return appErrs.BadRequest("selesaikan wawancara bisnis & pola stok dulu sebelum mengaktifkan modul persediaan")
	}
	if _, ok := normalizeCostingMethod(rec.Method); !ok {
		return appErrs.BadRequest("dapatkan rekomendasi HPP dari wawancara dulu sebelum mengaktifkan modul")
	}
	return nil
}
