# go-codestacker


| Route                     | Method | Description                              | Auth Required |
|---------------------------|--------|------------------------------------------|---------------|
| `/register`               | POST   | Register a new user                      | No            |
| `/login`                  | POST   | Login with user credentials              | Yes           |
| `/upload`                 | POST   | Upload a PDF file                        | Yes           |
| `/pdfs`                   | GET    | List all PDFs                            | Yes           |
| `/pdfs/:id`               | GET    | Get a specific PDF by ID                 | Yes           |
| `/search`                 | GET    | Search for a keyword in all PDFs         | Yes           |
| `/pdfs/:id/page/:page`    | GET    | Get a specific page as an image          | Yes           |
| `/pdfs/:id`               | DELETE | Delete a specific PDF and its related data | Yes           |
