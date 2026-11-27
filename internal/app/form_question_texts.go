package app

// formQuestionTexts checks a question's own words past its name: its
// description, and on a checkbox question the label beside its box, which a
// Notion form's builder asks for as "체크박스 라벨(선택 사항)" and its
// respondent reads after the box (seen 2026-11-17). A question of another
// kind has no such label.
func formQuestionTexts(at string, question formQuestion, property storedFormProperty) error {
	if err := formTextField(at+".description", question.Description, 2*1024); err != nil {
		return err
	}
	if question.CheckboxLabel != "" && property.Type != graphValueTypeCheckbox {
		return formInvalid("%s.checkboxLabel is only for a question on a checkbox property; %q is %s", at, property.Name, property.Type)
	}
	return formTextField(at+".checkboxLabel", question.CheckboxLabel, 512)
}
