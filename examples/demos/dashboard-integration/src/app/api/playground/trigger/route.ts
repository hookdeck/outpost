import { NextRequest, NextResponse } from "next/server";
import { getToken } from "next-auth/jwt";
import { getOutpostClient } from "@/lib/outpost";
import { z } from "zod";
import logger from "@/lib/logger";

// Request body validation schema
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
    // Check authentication using JWT token (same pattern as overview route)
    const token = await getToken({
      req: request,
      secret: process.env.NEXTAUTH_SECRET,
    });

    if (!token?.id) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    // Parse and validate request body
    let body;
    try {
      body = await request.json();
    } catch {
      return NextResponse.json(
        { error: "Invalid JSON in request body" },
        { status: 400 }
      );
    }

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
    const tenantId = token.id as string; // Use user ID as tenant ID

    logger.info("Triggering test event", {
      tenantId,
      destinationId,
      topic,
      userId: tenantId,
    });

    // Get Outpost client
    let outpostClient;
    try {
      outpostClient = getOutpostClient();
    } catch (error) {
      logger.error("Failed to initialize Outpost client:", error);
      return NextResponse.json(
        { error: "Failed to initialize event service" },
        { status: 500 }
      );
    }

    try {
      // Prepare event payload with metadata
      const eventPayload = {
        ...eventData,
        // Add metadata about who triggered this test event
        _test_metadata: {
          triggeredBy: tenantId,
          triggeredAt: new Date().toISOString(),
          isTestEvent: true,
          destinationId,
        },
      };

      // Publish the event using the Outpost SDK
      // Based on the SDK examples, use publish.event method
      const result = await outpostClient.publish.event({
        tenantId,
        topic,
        data: eventPayload,
        destinationId, // Route to specific destination
      });

      logger.info("Test event triggered successfully", {
        tenantId,
        destinationId,
        topic,
        eventId: result?.id,
      });

      return NextResponse.json({
        success: true,
        message: "Test event triggered successfully",
        eventId: result?.id || "unknown",
        timestamp: new Date().toISOString(),
        destinationId,
        topic,
      });
    } catch (outpostError: any) {
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

      if (outpostError?.response?.status === 403) {
        return NextResponse.json(
          { error: "Access denied to destination" },
          { status: 403 }
        );
      }

      if (outpostError?.response?.status === 400) {
        return NextResponse.json(
          { error: "Invalid event data or destination configuration" },
          { status: 400 }
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
  } catch (error: any) {
    logger.error("Unexpected error in playground trigger API:", {
      error,
      errorMessage: error?.message,
    });

    return NextResponse.json(
      {
        error: "Internal server error",
        details:
          process.env.NODE_ENV === "development" ? error?.message : undefined,
      },
      { status: 500 }
    );
  }
}

// Only allow POST method - return 405 for other methods
export async function GET() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "POST" } }
  );
}

export async function PUT() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "POST" } }
  );
}

export async function DELETE() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "POST" } }
  );
}

export async function PATCH() {
  return NextResponse.json(
    { error: "Method not allowed" },
    { status: 405, headers: { Allow: "POST" } }
  );
}
