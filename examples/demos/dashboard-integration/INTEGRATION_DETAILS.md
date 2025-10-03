# Integrating Outpost Portal into Your Dashboard Application

A practical guide for developers implementing Outpost portal integration into their dashboard applications. This document provides specific integration flows, implementation details, and working code examples.

## Table of Contents

- [Integration Architecture Overview](#integration-architecture-overview)
- [Core Integration Points](#core-integration-points)
  - [1. Automatic Tenant Creation](#1-automatic-tenant-creation)
  - [2. Portal Access via Route Handlers](#2-portal-access-via-route-handlers)
  - [3. Dashboard Data Integration](#3-dashboard-data-integration)
  - [4. Event Testing Playground](#4-event-testing-playground)
- [SDK Setup and Configuration](#sdk-setup-and-configuration)
- [Authentication Integration](#authentication-integration)
- [Implementation Sequence Guide](#implementation-sequence-guide)

## Integration Architecture Overview

The integration follows a seamless embedded approach where Outpost portal functionality appears as a native part of your dashboard application.

```mermaid
graph TB
    User[User] --> Dashboard[Your Dashboard]
    Dashboard --> Portal[Outpost Portal]
    Dashboard --> SDK[Outpost SDK]
    SDK --> API[Outpost API]
    Portal --> API
    API --> Outpost[Outpost Services]

    subgraph "Your Application"
        Dashboard
        SDK
    end

    subgraph "Outpost Services"
        Portal
        API
        Outpost
    end
```

### User Flow

```mermaid
sequenceDiagram
    participant U as User
    participant D as Dashboard
    participant O as Outpost

    U->>D: Register Account
    D->>O: Create Tenant
    U->>D: View Dashboard
    D->>O: Fetch Overview Data
    U->>D: Click "Event Destinations"
    D->>O: Generate Portal URL
    D->>U: Redirect to Portal
    U->>O: Manage Destinations
    O->>D: Return to Dashboard
```

### Authentication Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant A as API Route
    participant S as Outpost SDK

    C->>A: API Request + JWT
    A->>A: Validate JWT Token
    A->>A: Extract User ID as Tenant ID
    A->>S: Call Outpost API
    S->>A: Return Data
    A->>C: Return Response
```

## Core Integration Points

### 1. Automatic Tenant Creation

**Purpose:** Create Outpost tenants automatically when users register for your platform

#### Implementation Strategy

- Trigger tenant creation during user registration
- Use platform user ID directly as Outpost tenant ID
- Handle tenant creation failure

#### Code Implementation

**Registration API Route** (`src/app/api/auth/register/route.ts`):

```typescript
import { getOutpostClient } from "@/lib/outpost";
import { generateTenantId } from "@/lib/utils";

export async function POST(request: NextRequest) {
  // ... user validation and creation logic ...

  // Hash password and create user
  const hashedPassword = await bcrypt.hash(password, 12);
  const user = await db.user.create({
    data: { name, email, password: hashedPassword },
  });

  // Create Outpost tenant automatically
  try {
    const tenantId = generateTenantId(user.id.toString());
    await createTenant(tenantId);
    logger.info("Outpost tenant created successfully", {
      userId: user.id,
      tenantId,
    });
  } catch (error) {
    // How you handle this is up to you. Notify the team and perform a resync.
    logger.error("Failed to create Outpost tenant", { userId: user.id, error });
  }

  return NextResponse.json({
    message: "User registered successfully",
    user: { id: user.id, name: user.name, email: user.email },
  });
}
```

**Tenant Creation Function** (`src/lib/outpost.ts`):

```typescript
export async function createTenant(tenantId: string): Promise<void> {
  const outpost = getOutpostClient();

  try {
    await outpost.tenants.upsert({ tenantId });
    logger.info("Tenant created/updated successfully", { tenantId });
  } catch (error) {
    logger.error("Failed to create tenant", { tenantId, error });
    throw error;
  }
}
```

**Tenant ID Generation** (`src/lib/utils.ts`):

```typescript
export function generateTenantId(userId: string): string {
  // Simple 1:1 mapping - use user ID directly as tenant ID
  return userId;
}
```

#### Key Points

- **1:1 Mapping**: User ID directly becomes tenant ID for simplicity
- **Graceful Degradation**: Registration succeeds even if tenant creation fails
- **Logging**: Comprehensive logging for debugging and monitoring

### 2. Portal Access via Route Handlers

**Purpose:** Seamlessly redirect users to Outpost portal for destination management

#### Implementation Strategy

- Create Next.js route handlers that generate portal URLs
- Support deep linking to specific portal pages
- Maintain user session across the redirect
- Support theme customization for branding

#### Portal Routes Structure

```
/dashboard/event-destinations           → Portal root
/dashboard/event-destinations/new      → Create destination
/dashboard/event-destinations/destinations/{id} → Edit destination
```

#### Code Implementation

**Root Portal Access** (`src/app/dashboard/event-destinations/route.ts`):

```typescript
import { getToken } from "next-auth/jwt";
import { getPortalUrl } from "@/lib/outpost";

export async function GET(request: NextRequest) {
  // Authenticate user
  const token = await getToken({
    req: request,
    secret: process.env.NEXTAUTH_SECRET,
  });

  if (!token?.id) {
    return NextResponse.redirect(new URL("/auth/login", request.url));
  }

  const tenantId = token.id as string;
  const { searchParams } = new URL(request.url);
  const theme = searchParams.get("theme") || "light";

  try {
    // Generate authenticated portal URL
    const portalUrl = await getPortalUrl(tenantId, theme);
    return NextResponse.redirect(portalUrl);
  } catch (error) {
    logger.error("Failed to generate portal URL", { tenantId, error });
    return NextResponse.redirect(
      new URL("/dashboard?error=portal-access", request.url)
    );
  }
}
```

**Deep Linking Support** (`src/app/dashboard/event-destinations/[...path]/route.ts`):

```typescript
export async function GET(
  request: NextRequest,
  { params }: { params: { path: string[] } }
) {
  const token = await getToken({
    req: request,
    secret: process.env.NEXTAUTH_SECRET,
  });

  if (!token?.id) {
    return NextResponse.redirect(new URL("/auth/login", request.url));
  }

  const tenantId = token.id as string;
  const path = params.path.join("/");

  // Validate allowed portal paths
  const allowedPaths = ["new", "destinations"];
  const pathSegments = path.split("/");

  if (!isValidPortalPath(pathSegments, allowedPaths)) {
    // Invalid path - redirect to portal root
    return NextResponse.redirect(
      new URL("/dashboard/event-destinations", request.url)
    );
  }

  const { searchParams } = new URL(request.url);
  const theme = searchParams.get("theme") || "light";

  try {
    // Generate portal URL with specific path
    const portalUrl = await getPortalUrl(tenantId, theme, path);
    return NextResponse.redirect(portalUrl);
  } catch (error) {
    logger.error("Failed to generate portal URL with path", {
      tenantId,
      path,
      error,
    });
    return NextResponse.redirect(
      new URL("/dashboard?error=portal-access", request.url)
    );
  }
}

function isValidPortalPath(
  segments: string[],
  allowedPaths: string[]
): boolean {
  if (segments.length === 0) return false;

  // Allow /new
  if (segments.length === 1 && segments[0] === "new") return true;

  // Allow /destinations/{uuid}
  if (segments.length === 2 && segments[0] === "destinations") {
    const uuidRegex =
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    return uuidRegex.test(segments[1]);
  }

  return false;
}
```

**Portal URL Generation** (`src/lib/outpost.ts`):

```typescript
export async function getPortalUrl(
  tenantId: string,
  theme: string = "light",
  path?: string
): Promise<string> {
  const outpost = getOutpostClient();

  try {
    const response = await outpost.tenants.getPortalUrl({
      tenantId,
      theme: theme as "light" | "dark",
      ...(path && { path }),
    });

    return response.url;
  } catch (error) {
    logger.error("Failed to get portal URL", { tenantId, theme, path, error });
    throw error;
  }
}
```

#### Navigation Integration

**Sidebar Component** (`src/components/layout/Sidebar.tsx`):

```typescript
const navigation = [
  { name: "Overview", href: "/dashboard", icon: HomeIcon },
  {
    name: "Event Destinations",
    href: "/dashboard/event-destinations",
    icon: CloudIcon,
  },
  { name: "Playground", href: "/dashboard/playground", icon: BeakerIcon },
];

export default function Sidebar() {
  const pathname = usePathname();

  return (
    <nav className="space-y-1">
      {navigation.map((item) => {
        const isActive =
          pathname === item.href ||
          (item.href === "/dashboard/event-destinations" &&
            pathname.startsWith("/dashboard/event-destinations"));

        return (
          <Link
            key={item.name}
            href={item.href}
            className={cn(
              "group flex items-center px-2 py-2 text-sm font-medium rounded-md",
              isActive
                ? "bg-gray-100 text-gray-900"
                : "text-gray-600 hover:bg-gray-50 hover:text-gray-900"
            )}
          >
            <item.icon className="mr-3 h-6 w-6" />
            {item.name}
          </Link>
        );
      })}
    </nav>
  );
}
```

#### Key Points

- **Seamless Redirection**: Users don't see intermediate loading states
- **Deep Linking**: Support for specific portal pages with path validation
- **Theme Support**: Portal matches your application's theme
- **Security**: Path validation prevents unauthorized portal access

### 3. Dashboard Data Integration

**Purpose:** Display Outpost data (destinations, events, statistics) in your dashboard

#### Implementation Strategy

- Create API endpoint that aggregates data from multiple Outpost APIs
- Use React hooks for data fetching and state management
- Transform Outpost data to match your UI requirements
- Display real-time statistics and recent activity

#### Code Implementation

**Dashboard API Endpoint** (`src/app/api/overview/route.ts`):

```typescript
import { getTenantOverview } from "@/lib/outpost";

export async function GET(request: NextRequest) {
  const token = await getToken({
    req: request,
    secret: process.env.NEXTAUTH_SECRET,
  });

  if (!token?.id) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const tenantId = token.id as string;

  try {
    const overview = await getTenantOverview(tenantId);
    return NextResponse.json(overview);
  } catch (error) {
    logger.error("Failed to fetch tenant overview", { tenantId, error });
    return NextResponse.json(
      { error: "Failed to fetch overview" },
      { status: 500 }
    );
  }
}
```

**Data Aggregation Function** (`src/lib/outpost.ts`):

```typescript
export async function getTenantOverview(
  tenantId: string
): Promise<DashboardOverview> {
  const outpost = getOutpostClient();

  try {
    // Fetch tenant info
    const tenant = await outpost.tenants.get({ tenantId });

    // Fetch destinations
    const destinationsResponse = await outpost.destinations.list({ tenantId });
    const destinations = destinationsResponse.map(transformDestination);

    // Fetch recent events
    const eventsResponse = await outpost.events.list({
      tenantId,
      limit: 5,
      orderBy: "createdAt",
      orderDirection: "desc",
    });
    const recentEvents = eventsResponse.items.map(transformEvent);

    // Calculate statistics
    const stats = {
      totalDestinations: destinations.length,
      totalEvents: eventsResponse.totalCount || 0,
    };

    return {
      tenant: { id: tenantId },
      destinations,
      recentEvents,
      stats,
    };
  } catch (error) {
    // Handle SDK validation errors
    if (error.rawValue) {
      logger.warn("SDK validation error, using raw value", {
        error: error.rawValue,
      });
      return error.rawValue;
    }
    throw error;
  }
}

function transformDestination(dest: any): Destination {
  return {
    id: dest.id,
    type: dest.type,
    config: dest.config || {},
    topics: dest.topics || [],
    enabled: !dest.disabledAt, // Transform disabledAt to enabled boolean
    createdAt: dest.createdAt,
  };
}

function transformEvent(event: any): Event {
  return {
    id: event.id,
    topic: event.topic,
    data: event.data || {},
    createdAt: event.createdAt,
    status: event.status || "pending",
  };
}
```

**React Hook for Data Fetching** (`src/hooks/useOverview.ts`):

```typescript
import { useState, useEffect } from "react";
import type { DashboardOverview } from "@/types/dashboard";

export function useOverview() {
  const [data, setData] = useState<DashboardOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchOverview() {
      try {
        const response = await fetch("/api/overview");
        if (!response.ok) {
          throw new Error("Failed to fetch overview");
        }
        const overview = await response.json();
        setData(overview);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
      } finally {
        setLoading(false);
      }
    }

    fetchOverview();
  }, []);

  const refetch = async () => {
    setLoading(true);
    setError(null);
    // ... refetch logic ...
  };

  return { data, loading, error, refetch };
}
```

**UI Component Integration** (`src/components/dashboard/OverviewStats.tsx`):

```typescript
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";

interface OverviewStatsProps {
  stats: {
    totalDestinations: number;
    totalEvents: number;
  };
}

export default function OverviewStats({ stats }: OverviewStatsProps) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">
            Total Destinations
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{stats.totalDestinations}</div>
          <p className="text-xs text-muted-foreground">
            Active webhook destinations
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Events</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{stats.totalEvents}</div>
          <p className="text-xs text-muted-foreground">Events processed</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">System Status</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center space-x-2">
            <Badge variant="success">Active</Badge>
          </div>
          <p className="text-xs text-muted-foreground">
            All systems operational
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Quick Actions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            <Link
              href="/dashboard/event-destinations"
              className="block text-sm text-blue-600 hover:text-blue-500"
            >
              Manage destinations →
            </Link>
            <Link
              href="/dashboard/event-destinations/new"
              className="block text-sm text-blue-600 hover:text-blue-500"
            >
              Create destination →
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
```

#### Key Points

- **Data Aggregation**: Single API call fetches data from multiple Outpost endpoints
- **Data Transformation**: Convert Outpost data structures to match your UI needs
- **Real-time Display**: Hook pattern enables automatic data updates
- **Error Handling**: Graceful handling of SDK validation errors

### 4. Event Testing Playground

**Purpose:** Allow users to test event publishing directly from your dashboard

#### Implementation Strategy

- Provide interactive form for event testing
- Load user's destinations and filter topics based on selection
- Support real-time event publishing with immediate feedback
- Validate event data and provide helpful error messages

#### Code Implementation

**Playground Component** (`src/app/dashboard/playground/page.tsx`):

```typescript
import { useState, useEffect } from "react";
import { useSession } from "next-auth/react";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/Card";
import { Select } from "@/components/ui/Select";
import { useDestinations } from "@/hooks/useDestinations";
import { useTopics } from "@/hooks/useTopics";

export default function PlaygroundPage() {
  const { status } = useSession();
  const router = useRouter();
  const { data: destinations, loading: destinationsLoading } =
    useDestinations();
  const { loading: topicsLoading } = useTopics();

  const [formData, setFormData] = useState({
    destinationId: "",
    topic: "",
    eventData: JSON.stringify(
      {
        example: "data",
        timestamp: new Date().toISOString(),
        userId: "user-123",
      },
      null,
      2
    ),
  });

  const [response, setResponse] = useState(null);
  const [isLoading, setIsLoading] = useState(false);

  // Get selected destination and available topics
  const selectedDestination = destinations.find(
    (dest) => dest.id === formData.destinationId
  );
  const availableTopics = selectedDestination?.topics || [];

  // Clear topic when destination changes if topic not available
  useEffect(() => {
    if (
      formData.destinationId &&
      formData.topic &&
      selectedDestination &&
      !selectedDestination.topics.includes(formData.topic)
    ) {
      setFormData((prev) => ({ ...prev, topic: "" }));
    }
  }, [formData.destinationId, formData.topic, selectedDestination]);

  const handleSubmit = async (e) => {
    e.preventDefault();

    if (!validateForm()) return;

    setIsLoading(true);
    setResponse(null);

    try {
      const payload = {
        destinationId: formData.destinationId,
        topic: formData.topic,
        eventData: JSON.parse(formData.eventData),
      };

      const response = await fetch("/api/playground/trigger", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      const data = await response.json();
      setResponse({ success: response.ok, ...data });

      if (response.ok) {
        // Reset form on success
        setFormData((prev) => ({
          ...prev,
          destinationId: "",
          topic: "",
        }));
      }
    } catch (error) {
      setResponse({
        success: false,
        error: "Network error occurred",
      });
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Event Playground</h1>
        <p className="text-gray-600">
          Test event triggering by sending custom events to your configured
          destinations.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Trigger Event</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-6">
              {/* Destination Selector */}
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Destination
                </label>
                <Select
                  value={formData.destinationId}
                  onChange={(e) =>
                    setFormData((prev) => ({
                      ...prev,
                      destinationId: e.target.value,
                      topic: "", // Clear topic when destination changes
                    }))
                  }
                  disabled={destinationsLoading}
                >
                  <option value="">
                    {destinationsLoading
                      ? "Loading destinations..."
                      : "Select a destination"}
                  </option>
                  {destinations.map((destination) => (
                    <option key={destination.id} value={destination.id}>
                      {destination.type} - {destination.config.url}
                      {!destination.enabled ? " (Disabled)" : ""}
                    </option>
                  ))}
                </Select>
              </div>

              {/* Topic Selector - Filtered by destination */}
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Topic
                </label>
                <Select
                  value={formData.topic}
                  onChange={(e) =>
                    setFormData((prev) => ({ ...prev, topic: e.target.value }))
                  }
                  disabled={topicsLoading || !formData.destinationId}
                >
                  <option value="">
                    {topicsLoading
                      ? "Loading topics..."
                      : !formData.destinationId
                      ? "Select a destination first"
                      : availableTopics.length === 0
                      ? "No topics available for this destination"
                      : "Select a topic"}
                  </option>
                  {formData.destinationId &&
                    availableTopics.map((topic) => (
                      <option key={topic} value={topic}>
                        {topic}
                      </option>
                    ))}
                </Select>
                {formData.destinationId && availableTopics.length === 0 && (
                  <p className="mt-1 text-sm text-amber-600">
                    This destination is not subscribed to any topics. Configure
                    topics in the destination settings.
                  </p>
                )}
              </div>

              {/* JSON Payload Editor */}
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Event Data (JSON)
                </label>
                <textarea
                  value={formData.eventData}
                  onChange={(e) =>
                    setFormData((prev) => ({
                      ...prev,
                      eventData: e.target.value,
                    }))
                  }
                  rows={12}
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm font-mono text-sm"
                  placeholder="Enter valid JSON..."
                />
              </div>

              <Button
                type="submit"
                disabled={isLoading || destinationsLoading}
                className="w-full"
                size="lg"
              >
                {isLoading ? "Triggering Event..." : "Trigger Event"}
              </Button>
            </form>
          </CardContent>
        </Card>

        {/* Response Display */}
        <Card>
          <CardHeader>
            <CardTitle>Response</CardTitle>
          </CardHeader>
          <CardContent>
            {response ? (
              <div className="space-y-4">
                <div className="flex items-center">
                  <span
                    className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                      response.success
                        ? "bg-green-100 text-green-800"
                        : "bg-red-100 text-red-800"
                    }`}
                  >
                    {response.success ? "Success" : "Error"}
                  </span>
                </div>

                {response.message && (
                  <div
                    className={`p-4 rounded-md ${
                      response.success
                        ? "bg-green-50 text-green-700"
                        : "bg-red-50 text-red-700"
                    }`}
                  >
                    <p className="text-sm">{response.message}</p>
                  </div>
                )}

                {response.success && response.eventId && (
                  <div className="bg-gray-50 border rounded-md p-4">
                    <h3 className="text-sm font-medium text-gray-900 mb-2">
                      Event Details:
                    </h3>
                    <dl className="text-sm space-y-1">
                      <div className="flex">
                        <dt className="font-medium text-gray-600 w-20">
                          Event ID:
                        </dt>
                        <dd className="text-gray-900 font-mono text-xs">
                          {response.eventId}
                        </dd>
                      </div>
                      <div className="flex">
                        <dt className="font-medium text-gray-600 w-20">
                          Topic:
                        </dt>
                        <dd className="text-gray-900">{response.topic}</dd>
                      </div>
                    </dl>
                  </div>
                )}
              </div>
            ) : (
              <div className="text-center py-12">
                <h3 className="mt-2 text-sm font-medium text-gray-900">
                  No response yet
                </h3>
                <p className="mt-1 text-sm text-gray-500">
                  Trigger an event to see the response here.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
```

**Event Publishing API** (`src/app/api/playground/trigger/route.ts`):

```typescript
import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getOutpostClient } from "@/lib/outpost";
import { z } from "zod";

const triggerEventSchema = z.object({
  destinationId: z.string().min(1, "Destination ID is required"),
  eventData: z
    .record(z.string(), z.any())
    .refine(
      (data) => Object.keys(data).length > 0,
      "Event data cannot be empty"
    ),
  topic: z.string().min(1, "Topic is required"),
});

export async function POST(request: NextRequest) {
  try {
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const body = await request.json();
    const validationResult = triggerEventSchema.safeParse(body);

    if (!validationResult.success) {
      return NextResponse.json(
        {
          error: "Validation failed",
          details: validationResult.error.issues,
        },
        { status: 400 }
      );
    }

    const { destinationId, eventData, topic } = validationResult.data;
    const tenantId = token.id as string;

    const outpostClient = getOutpostClient();

    try {
      // Prepare event payload with metadata
      const eventPayload = {
        ...eventData,
        _test_metadata: {
          triggeredBy: tenantId,
          triggeredAt: new Date().toISOString(),
          isTestEvent: true,
          destinationId,
        },
      };

      // Publish event using Outpost SDK
      const result = await outpostClient.publish.event({
        tenantId,
        topic,
        data: eventPayload,
        destinationId,
      });

      return NextResponse.json({
        success: true,
        message: "Test event triggered successfully",
        eventId: result?.id || "unknown",
        timestamp: new Date().toISOString(),
        destinationId,
        topic,
      });
    } catch (outpostError) {
      logger.error("Failed to publish event via Outpost:", {
        error: outpostError,
        tenantId,
        destinationId,
        topic,
      });

      // Handle specific Outpost SDK errors
      if (outpostError?.response?.status === 404) {
        return NextResponse.json(
          { error: "Destination not found or does not belong to your tenant" },
          { status: 404 }
        );
      }

      return NextResponse.json(
        {
          error: "Failed to publish event",
          details: outpostError?.message || "Unknown error occurred",
        },
        { status: 500 }
      );
    }
  } catch (error) {
    logger.error("Unexpected error in playground trigger API:", { error });
    return NextResponse.json(
      { error: "Internal server error" },
      { status: 500 }
    );
  }
}
```

**Data Hooks** (`src/hooks/useDestinations.ts`):

```typescript
import { useState, useEffect } from "react";
import type { Destination } from "@/types/dashboard";

export function useDestinations() {
  const [data, setData] = useState<Destination[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchDestinations() {
      try {
        const response = await fetch("/api/overview");
        if (!response.ok) {
          throw new Error("Failed to fetch destinations");
        }
        const overview = await response.json();
        if (overview.destinations && Array.isArray(overview.destinations)) {
          setData(overview.destinations);
        } else {
          setData([]);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
        setData([]);
      } finally {
        setLoading(false);
      }
    }

    fetchDestinations();
  }, []);

  return { data, loading, error };
}
```

#### Key Features

- **Smart Topic Filtering**: Topics are filtered based on selected destination's subscriptions
- **Real-time Validation**: Form validates JSON syntax and required fields
- **Metadata Injection**: Test events include debugging metadata
- **Response Display**: Shows detailed success/error information
- **Progressive Enhancement**: Form fields enable/disable based on state

## SDK Setup and Configuration

### Installation and Setup

**Install Outpost SDK:**

```bash
npm install @outpost/sdk
```

**SDK Client Configuration** (`src/lib/outpost.ts`):

```typescript
import { Outpost } from "@outpost/sdk";

let outpostClient: Outpost | null = null;

export function getOutpostClient(): Outpost {
  if (!outpostClient) {
    const baseUrl = process.env.OUTPOST_BASE_URL;
    const apiKey = process.env.OUTPOST_API_KEY;

    if (!baseUrl || !apiKey) {
      throw new Error("Missing required Outpost configuration");
    }

    outpostClient = new Outpost({
      serverURL: `${baseUrl}/api/v1`,
      security: {
        adminApiKey: apiKey,
      },
    });
  }

  return outpostClient;
}
```

### Environment Variables

**Dashboard Application** (`.env.local`):

```bash
# Outpost Integration
OUTPOST_BASE_URL=http://localhost:3333
OUTPOST_API_KEY=demo-api-key-change-in-production

# Authentication
NEXTAUTH_SECRET=your-secret-here

# Database
DATABASE_URL=postgresql://dashboard:dashboard@localhost:5432/dashboard_integration
```

**Outpost Configuration** (`.env.outpost`):

```bash
# API Configuration
API_KEY=demo-api-key-change-in-production
API_JWT_SECRET=demo-jwt-secret-change-in-production

# Portal Branding
PORTAL_ORGANIZATION_NAME=Your Platform Name
PORTAL_REFERER_URL=http://localhost:3000

# Event Topics
TOPICS=user.created,user.updated,order.completed,payment.processed
```

## Authentication Integration

### JWT Token Pattern

All API routes follow this authentication pattern:

```typescript
import { getToken } from "next-auth/jwt";

export async function GET(request: NextRequest) {
  // Extract and validate JWT token
  const token = await getToken({
    req: request,
    secret: process.env.NEXTAUTH_SECRET,
  });

  if (!token?.id) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  // Use user ID directly as tenant ID
  const tenantId = token.id as string;

  // Make Outpost API calls with tenant ID
  // ...
}
```

### User to Tenant Mapping

```typescript
// Simple 1:1 mapping strategy
function userIdToTenantId(userId: string): string {
  return userId; // Direct mapping for simplicity
}

// Alternative: Use a prefix or transformation
function userIdToTenantId(userId: string): string {
  return `tenant_${userId}`;
}
```

## Data Models and Types

**Core Type Definitions** (`src/types/dashboard.ts`):

```typescript
export interface DashboardOverview {
  tenant: {
    id: string;
  };
  destinations: Destination[];
  recentEvents: Event[];
  stats: {
    totalDestinations: number;
    totalEvents: number;
  };
}

export interface Destination {
  id: string;
  type: string;
  config: {
    url?: string;
  };
  topics: string[];
  enabled: boolean;
  createdAt: string;
}

export interface Event {
  id: string;
  topic: string;
  data: Record<string, unknown>;
  createdAt: string;
  status: "pending" | "delivered" | "failed";
}
```

## Implementation Sequence Guide

### Step 1: SDK Setup

1. Install Outpost SDK package
2. Configure environment variables
3. Set up client initialization
4. Test basic connectivity

### Step 2: Tenant Management

1. Add tenant creation to user registration flow
2. Implement user ID to tenant ID mapping
3. Test tenant provisioning
4. Handle graceful degradation

### Step 3: Portal Integration

1. Create portal route handlers
2. Add navigation links to your dashboard
3. Test portal redirection
4. Implement deep linking support

### Step 4: Dashboard Data Integration

1. Create overview API endpoint
2. Implement data fetching hooks
3. Add UI components for data display
4. Test real-time data updates

### Step 5: Event Testing (Optional)

1. Create playground interface
2. Implement event publishing endpoint
3. Add topic fetching and filtering
4. Test complete event flow

### Verification Checklist

- [ ] User registration creates Outpost tenant automatically
- [ ] Portal navigation redirects to authenticated Outpost portal
- [ ] Dashboard displays real-time destination and event data
- [ ] Portal returns to dashboard via "Back to" navigation
- [ ] Event playground successfully publishes test events
- [ ] Topics are filtered based on destination subscriptions
- [ ] All API routes properly authenticate requests
- [ ] Portal maintains your application's branding/theme

## File Structure Reference

```
src/
├── lib/
│   └── outpost.ts                     # SDK setup and core functions
├── app/api/
│   ├── auth/register/route.ts         # Tenant creation during registration
│   ├── overview/route.ts              # Dashboard data aggregation
│   ├── topics/route.ts                # Available topics
│   └── playground/trigger/route.ts   # Event publishing
├── app/dashboard/
│   ├── page.tsx                       # Dashboard overview page
│   ├── playground/page.tsx            # Event testing interface
│   └── event-destinations/
│       ├── route.ts                   # Portal redirection
│       └── [...path]/route.ts         # Deep linking support
├── hooks/
│   ├── useOverview.ts                 # Dashboard data fetching
│   ├── useDestinations.ts             # Destinations data
│   └── useTopics.ts                   # Topics data
├── components/
│   ├── layout/Sidebar.tsx             # Navigation integration
│   └── dashboard/OverviewStats.tsx    # Statistics display
└── types/
    └── dashboard.ts                   # Type definitions
```

This guide provides the essential integration patterns needed to embed Outpost portal functionality into your dashboard application. Each integration point can be implemented independently, allowing for incremental adoption of Outpost capabilities.
