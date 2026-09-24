# User Interaction Tools

Kit exposes small interactive tools that let the model collect one focused answer from the user through Kit-owned UI instead of asking the user to type a response in chat.

Available tools:

- `confirm_from_user` — asks for a yes/no confirmation and returns `details.confirmed`
- `input_from_user` — asks for one short freeform answer and returns `details.value`
- `select_from_user` — asks the user to choose one option and returns `details.value` and `details.label`

Cancellation behavior:

- `confirm_from_user` returns `{ confirmed: false }`
- `input_from_user` returns `{ value: null, cancelled: true }`
- `select_from_user` returns `{ value: null, label: null, cancelled: true }`

The model should use `guided_questions` instead when it needs two or more answers or a multi-step questionnaire.
