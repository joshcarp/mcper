# LinkedIn MCP Server

A WebAssembly (WASM) MCP server that provides tools to interact with the LinkedIn API for searching people, companies, and managing connections.

## Features

- **People Search**: Search for LinkedIn users with various filters (location, industry, company, title)
- **Profile Details**: Get detailed information about specific LinkedIn profiles
- **Company Search**: Search for companies on LinkedIn
- **Connections Management**: Retrieve user's LinkedIn connections

## Prerequisites

1. **LinkedIn API Access**: You need a LinkedIn API access token
2. **Go 1.23+**: For building the WASM binary
3. **WASI SDK**: For WebAssembly compilation

## Building the WASM Binary

```bash
# Build the WASM binary
GOOS=wasip1 GOARCH=wasm go build -o mcplinkedin.wasm cmd/mcplinkedin/main.go
```

## LinkedIn API Setup

To use this MCP server, you'll need to:

1. **Create a LinkedIn App**: Go to [LinkedIn Developers](https://www.linkedin.com/developers/)
2. **Get API Credentials**: Obtain your Client ID and Client Secret
3. **Request Permissions**: Request appropriate scopes for your use case
4. **Generate Access Token**: Use OAuth 2.0 flow to get an access token

### Required LinkedIn API Scopes

- `openid` - Use your name and photo
- `profile` - Use your name and photo  
- `email` - Use the primary email address associated with your LinkedIn account
- `w_member_social` - Create, modify, and delete posts, comments, and reactions on your behalf

## Available Tools

### 1. linkedin_search_people

Search for people on LinkedIn with various filters.

**Parameters:**
- `access_token` (required): LinkedIn API access token
- `query` (required): Search query string
- `location` (optional): Filter by location
- `industry` (optional): Filter by industry
- `company` (optional): Filter by company
- `title` (optional): Filter by job title
- `limit` (optional): Maximum number of results (default: 10)

**Example:**
```json
{
  "access_token": "your_linkedin_access_token",
  "query": "software engineer",
  "location": "San Francisco",
  "industry": "Technology",
  "limit": 20
}
```

### 2. linkedin_get_profile

Get detailed profile information for a specific LinkedIn user.

**Parameters:**
- `access_token` (required): LinkedIn API access token
- `profile_id` (required): LinkedIn profile ID

**Example:**
```json
{
  "access_token": "your_linkedin_access_token",
  "profile_id": "urn:li:person:123456789"
}
```

### 3. linkedin_search_companies

Search for companies on LinkedIn.

**Parameters:**
- `access_token` (required): LinkedIn API access token
- `query` (required): Search query string
- `industry` (optional): Filter by industry
- `location` (optional): Filter by location
- `limit` (optional): Maximum number of results (default: 10)

**Example:**
```json
{
  "access_token": "your_linkedin_access_token",
  "query": "tech startups",
  "industry": "Technology",
  "location": "Silicon Valley",
  "limit": 15
}
```

### 4. linkedin_get_connections

Get the user's LinkedIn connections.

**Parameters:**
- `access_token` (required): LinkedIn API access token
- `limit` (optional): Maximum number of connections to retrieve (default: 50)

**Example:**
```json
{
  "access_token": "your_linkedin_access_token",
  "limit": 100
}
```

## Usage with MCP Client

1. **Load the WASM binary** in your MCP client
2. **Provide your LinkedIn access token** when calling the tools
3. **Use the search filters** to narrow down results

## Error Handling

The server includes comprehensive error handling for:
- Missing access tokens
- Invalid API responses
- Network timeouts
- Rate limiting (LinkedIn API limits)

## Rate Limiting

LinkedIn API has rate limits. The server includes:
- 30-second timeout for requests
- Proper error handling for rate limit responses
- Recommended limits for search results

## Security Notes

- **Never hardcode access tokens** in your code
- **Use environment variables** or secure storage for tokens
- **Rotate access tokens** regularly
- **Follow LinkedIn's API usage guidelines**

## Troubleshooting

### Common Issues

1. **"Access token is required"**: Make sure you're passing a valid LinkedIn access token
2. **"API request failed"**: Check your token permissions and LinkedIn API status
3. **"Search query is required"**: Ensure you're providing a search term

### Debug Mode

Enable debug logging by setting the log level in your MCP client configuration.

## API Endpoints Used

- `GET /v2/peopleSearch` - Search for people
- `GET /v2/people/{id}` - Get profile details
- `GET /v2/companySearch` - Search for companies
- `GET /v2/connections` - Get user connections

## Contributing

Feel free to contribute by:
- Adding new LinkedIn API endpoints
- Improving error handling
- Adding more search filters
- Enhancing response formatting

## License

This project follows the same license as the parent repository.
