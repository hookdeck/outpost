# API Platform Dashboard Integration Demo

A Next.js application demonstrating how to integrate Outpost with an API platform or SaaS API dashboard. This example shows how API platforms can seamlessly embed Outpost's event destination management capabilities into their existing user dashboards.

## Features

- **API Platform Dashboard**: Complete user dashboard for an API/SaaS platform
- **Integrated Event Management**: Seamless Outpost integration for webhook/event destinations
- **Automatic Tenant Provisioning**: Users get Outpost tenants created automatically when they sign up
- **Event Testing Playground**: Interactive interface for testing event publishing to configured destinations
- **Unified User Experience**: Event destination management feels like a native part of the platform
- **Production-Ready Patterns**: Demonstrates real-world integration patterns for API platforms

## Prerequisites

- Node.js 18+
- Docker and Docker Compose
- Git

## Setup

1. **Clone and navigate to the project**:

   ```bash
   cd examples/demos/dashboard-integration
   ```

2. **Install dependencies**:

   ```bash
   npm install
   ```

3. **Set up environment variables**:

   ```bash
   # Dashboard application configuration
   cp .env.example .env.local

   # Outpost configuration
   cp .env.outpost.example .env.outpost
   ```

   The default configurations should work for local development. For production, update the secrets in both files.

   Update the `TOPICS` environment variable in `.env.outpost` to include the event topics you want to support:

   ```bash
   TOPICS=user.created,user.updated,order.completed,payment.processed,subscription.created
   ```

   For a full list of Outpost configuration options, see [Outpost Configuration](https://outpost.hookdeck.com/docs/references/configuration)

4. **Start the complete stack** (PostgreSQL, Redis, RabbitMQ, and Outpost):

   ```bash
   docker-compose up -d
   ```

   This will start:

   - `postgres` - PostgreSQL with separate databases for dashboard and Outpost (port 5432)
   - `redis` - Redis for Outpost (port 6379)
   - `rabbitmq` - Message queue for Outpost (port 5672, management UI on 15672)
   - `outpost-api` - Outpost API service (port 3333)
   - `outpost-delivery` - Outpost delivery service
   - `outpost-log` - Outpost log service

5. **Wait for services to be healthy**:

   ```bash
   docker-compose ps
   ```

   Wait until all services show "healthy" status. This may take 1-2 minutes on first startup.

6. **Run the dashboard application**:

   ```bash
   npm run dev
   ```

7. **Access the application**:
   - Dashboard: [http://localhost:3000](http://localhost:3000)
   - Outpost API: `http://localhost:3333/api/v1`
   - RabbitMQ Management: [http://localhost:15672](http://localhost:15672) (guest/guest)

## Application Structure

### Pages
- `/` - Landing page with registration and login links
- `/auth/register` - User registration form
- `/auth/login` - User login form
- `/dashboard` - Main dashboard with overview statistics and recent events
- `/dashboard/playground` - Interactive event testing interface
- `/dashboard/event-destinations/[...path]` - Portal integration routes

### Core Functionality

#### User Registration and Authentication
- User registration creates platform account and automatically provisions Outpost tenant
- NextAuth.js handles authentication with credential-based login
- User ID from platform database becomes tenant ID in Outpost
- JWT tokens secure API routes

#### Dashboard Overview
- Displays real-time statistics from Outpost API (total destinations, total events)
- Shows recent events with status indicators
- Lists configured destinations with type, URL, and enabled status
- Provides navigation to destination management and playground

#### Event Playground
- Interactive form for testing event publishing
- Destination dropdown populated from user's configured destinations
- Topic dropdown shows only topics that the selected destination is subscribed to
- JSON payload editor with validation
- Real-time event publishing to selected destination
- Response display with success/error details

#### Portal Integration
- Seamless redirection to Outpost portal for destination management
- Server-side portal URL generation with authentication
- Deep linking support for specific portal pages
- Branded portal experience with return navigation

## API Endpoints

### Dashboard APIs
- `GET /api/overview` - Returns dashboard statistics and recent events from Outpost
- `GET /api/topics` - Returns available topics from Outpost configuration
- `POST /api/playground/trigger` - Publishes test events via Outpost SDK

### Authentication APIs
- `POST /api/auth/register` - Creates user account and Outpost tenant
- NextAuth.js provides login/logout/session endpoints

### Portal Integration
- `GET /dashboard/event-destinations` - Redirects to Outpost portal root
- `GET /dashboard/event-destinations/[...path]` - Redirects to specific portal pages

## Architecture

### Frontend (Next.js)
- React components with Tailwind CSS for styling
- NextAuth.js for authentication
- Custom hooks for data fetching (`useOverview`, `useTopics`)
- Responsive design with loading states and error handling

### Backend Integration
- Next.js API routes for server-side operations
- Outpost TypeScript SDK for all Outpost interactions
- PostgreSQL for platform user management (separate from Outpost database)
- JWT tokens for API authentication

### Integration Points

For detailed implementation guidance with code examples, see **[INTEGRATION_DETAILS.md](./INTEGRATION_DETAILS.md)**.

1. User registration triggers Outpost tenant creation
2. Dashboard aggregates data from Outpost API
3. Portal access generates authenticated Outpost URLs
4. Playground publishes events directly via Outpost SDK

## Environment Variables

### Dashboard Application (.env.local)

| Variable           | Description                                                  | Example                                                                 |
| ------------------ | ------------------------------------------------------------ | ----------------------------------------------------------------------- |
| `DATABASE_URL`     | Dashboard PostgreSQL connection                              | `postgresql://dashboard:dashboard@localhost:5432/dashboard_integration` |
| `NEXTAUTH_SECRET`  | Auth.js secret key                                           | `your-secret-here`                                                      |
| `OUTPOST_BASE_URL` | Outpost base URL                                             | `http://localhost:3333`                                                 |
| `OUTPOST_API_KEY`  | Outpost API key                                              | `demo-api-key-change-in-production`                                     |
| `LOG_LEVEL`        | Application logging level (`error`, `warn`, `info`, `debug`) | `info`                                                                  |

### Outpost Configuration (.env.outpost)

| Variable                   | Description                                           | Example                                            |
| -------------------------- | ----------------------------------------------------- | -------------------------------------------------- |
| `API_KEY`                  | Outpost API key                                       | `demo-api-key-change-in-production`                |
| `API_JWT_SECRET`           | JWT signing secret                                    | `demo-jwt-secret-change-in-production`             |
| `POSTGRES_URL`             | Outpost PostgreSQL connection                         | `postgres://outpost:outpost@postgres:5432/outpost` |
| `REDIS_HOST`               | Redis hostname                                        | `redis`                                            |
| `RABBITMQ_SERVER_URL`      | RabbitMQ connection                                   | `amqp://guest:guest@rabbitmq:5672`                 |
| `TOPICS`                   | Available event topics (comma-separated)             | `user.created,order.completed,payment.processed`   |
| `PORTAL_ORGANIZATION_NAME` | Portal branding                                       | `API Platform Demo`                                |
| `PORTAL_REFERER_URL`       | Dashboard URL for "Back to" navigation link in portal | `http://localhost:3000`                            |

## Docker Services

The docker-compose.yml includes the complete stack:

- **postgres** (port 5432): Single PostgreSQL instance with separate databases for dashboard (`dashboard_integration`) and Outpost (`outpost`)
- **redis** (port 6379): Redis for Outpost caching and sessions
- **rabbitmq** (ports 5672, 15672): Message queue for Outpost event processing
- **outpost-api** (port 3333): Outpost API service
- **outpost-delivery**: Outpost delivery service for processing webhooks
- **outpost-log**: Outpost log service for event logging

## Development

- `npm run dev` - Start development server
- `npm run build` - Build for production
- `npm run start` - Start production server
- `npm run lint` - Run ESLint
- `docker-compose up -d` - Start all services
- `docker-compose down` - Stop all services
- `docker-compose logs [service]` - View logs for a specific service

## Troubleshooting

### Services not starting

```bash
# Check service status
docker-compose ps

# View logs for a specific service
docker-compose logs outpost-api
docker-compose logs postgres

# Restart services
docker-compose restart
```

### Database connection issues

```bash
# Check PostgreSQL is ready
docker-compose exec postgres pg_isready -U dashboard -d dashboard_integration
docker-compose exec postgres pg_isready -U outpost -d outpost
```

### Outpost API not responding

```bash
# Check if Outpost API is healthy
curl http://localhost:3333/healthz

# View Outpost logs
docker-compose logs outpost-api
```

### Topics not loading in playground

Ensure the `TOPICS` environment variable is set in `.env.outpost`:

```bash
TOPICS=user.created,user.updated,order.completed,payment.processed
```

Restart the Outpost services after updating:

```bash
docker-compose restart outpost-api
```

## Use Case

This demo represents a common integration scenario where:

- **API Platform**: Your SaaS provides APIs that generate events (user signups, payments, data processing, etc.)
- **Customer Need**: Your customers need to receive these events via webhooks to their own systems
- **Outpost Integration**: Instead of building webhook infrastructure from scratch, you integrate Outpost
- **User Experience**: Your customers manage event destinations through what feels like your native platform

## Integration Patterns

This demo demonstrates several integration patterns:

- **Transparent Tenant Management**: Outpost tenants are created automatically during user registration and hidden from end users
- **Embedded Navigation**: Event destinations appear as a native part of the platform navigation
- **Portal Integration**: Server-side URL generation provides seamless access to Outpost's management interface
- **Data Aggregation**: Platform dashboard displays relevant Outpost statistics alongside application metrics
- **Event Testing**: Built-in playground allows testing events against real configured destinations

## Technical Implementation

### User Flow
1. User registers account → Creates Outpost tenant automatically
2. User logs in → Receives platform session with JWT
3. User views dashboard → Data aggregated from Outpost API
4. User manages destinations → Redirected to authenticated Outpost portal
5. User tests events → Playground publishes to actual destinations via Outpost SDK

### Security
- Password hashing with bcryptjs
- JWT token validation on API routes
- Zod schema validation for form inputs
- SQL injection prevention via parameterized queries
- Environment variables for API keys and secrets

### Error Handling
- Form validation with user-friendly error messages
- API error responses with appropriate HTTP status codes
- Loading states and skeleton screens for data fetching
- Fallback UI for failed API calls