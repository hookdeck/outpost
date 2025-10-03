"use client";

import { useState, useEffect } from "react";
import { useSession } from "next-auth/react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/Card";
import { Select } from "@/components/ui/Select";
import { useTopics } from "@/hooks/useTopics";
import { useDestinations } from "@/hooks/useDestinations";

interface FormData {
  destinationId: string;
  topic: string;
  eventData: string;
}

interface ApiResponse {
  success: boolean;
  message?: string;
  eventId?: string;
  timestamp?: string;
  destinationId?: string;
  topic?: string;
  error?: string;
  details?: any;
}

export default function PlaygroundPage() {
  const { status } = useSession();
  const router = useRouter();
  const { data: destinations, loading: destinationsLoading, error: destinationsError } = useDestinations();
  const { loading: topicsLoading, error: topicsError } = useTopics();
  const [formData, setFormData] = useState<FormData>({
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
  const [response, setResponse] = useState<ApiResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [formErrors, setFormErrors] = useState<Partial<FormData>>({});

  // Handle authentication
  useEffect(() => {
    if (status === "unauthenticated") {
      router.push("/auth/login");
    }
  }, [status, router]);

  // Get the currently selected destination
  const selectedDestination = destinations.find(
    (dest) => dest.id === formData.destinationId
  );

  // Get available topics for the selected destination
  const availableTopics = selectedDestination?.topics || [];

  // Clear topic selection when destination changes if current topic is not available
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

  const validateForm = (): boolean => {
    const errors: Partial<FormData> = {};

    if (!formData.destinationId) {
      errors.destinationId = "Destination is required";
    }

    if (!formData.topic.trim()) {
      errors.topic = "Topic is required";
    } else if (selectedDestination && !selectedDestination.topics.includes(formData.topic)) {
      errors.topic = "Selected topic is not available for this destination";
    }

    if (!formData.eventData.trim()) {
      errors.eventData = "Event data is required";
    } else {
      try {
        JSON.parse(formData.eventData);
      } catch {
        errors.eventData = "Event data must be valid JSON";
      }
    }

    setFormErrors(errors);
    return Object.keys(errors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!validateForm()) {
      return;
    }

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
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(payload),
      });

      const data = await response.json();

      setResponse({
        success: response.ok,
        ...data,
      });

      if (response.ok) {
        // Reset form on success, but keep a fresh JSON template
        setFormData({
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
        setFormErrors({});
      }
    } catch {
      setResponse({
        success: false,
        error: "Network error occurred",
      });
    } finally {
      setIsLoading(false);
    }
  };

  const handleInputChange = (field: keyof FormData, value: string) => {
    setFormData((prev) => ({ ...prev, [field]: value }));
    // Clear error when user starts typing
    if (formErrors[field]) {
      setFormErrors((prev) => ({ ...prev, [field]: undefined }));
    }
  };

  // Show loading while authenticating
  if (status === "loading") {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900"></div>
      </div>
    );
  }

  // Don't render if not authenticated (will redirect)
  if (status !== "authenticated") {
    return null;
  }

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
        {/* Event Trigger Form */}
        <Card>
          <CardHeader>
            <CardTitle>Trigger Event</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-6">
            {/* Destination Selector */}
            <div>
              <label
                htmlFor="destination"
                className="block text-sm font-medium text-gray-700"
              >
                Destination
              </label>
              <Select
                id="destination"
                value={formData.destinationId}
                onChange={(e) =>
                  handleInputChange("destinationId", e.target.value)
                }
                className={
                  formErrors.destinationId ? "border-red-300" : ""
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
                    {destination.type}{" "}
                    {destination.config.url
                      ? `- ${destination.config.url}`
                      : ""}
                    {!destination.enabled ? " (Disabled)" : ""}
                  </option>
                ))}
              </Select>
              {formErrors.destinationId && (
                <p className="mt-1 text-sm text-red-600">
                  {formErrors.destinationId}
                </p>
              )}
              {destinationsError && (
                <p className="mt-1 text-sm text-red-600">
                  Failed to load destinations: {destinationsError}
                </p>
              )}
            </div>

            {/* Topic Selector */}
            <div>
              <label
                htmlFor="topic"
                className="block text-sm font-medium text-gray-700"
              >
                Topic
              </label>
              <Select
                id="topic"
                value={formData.topic}
                onChange={(e) => handleInputChange("topic", e.target.value)}
                className={
                  formErrors.topic ? "border-red-300" : ""
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
              {formErrors.topic && (
                <p className="mt-1 text-sm text-red-600">{formErrors.topic}</p>
              )}
              {topicsError && (
                <p className="mt-1 text-sm text-red-600">
                  Failed to load topics: {topicsError}
                </p>
              )}
              {formData.destinationId && availableTopics.length === 0 && !topicsLoading && (
                <p className="mt-1 text-sm text-amber-600">
                  This destination is not subscribed to any topics. Configure topics in the destination settings.
                </p>
              )}
            </div>

            {/* JSON Payload Editor */}
            <div>
              <label
                htmlFor="eventData"
                className="block text-sm font-medium text-gray-700"
              >
                Event Data (JSON)
              </label>
              <textarea
                id="eventData"
                value={formData.eventData}
                onChange={(e) => handleInputChange("eventData", e.target.value)}
                rows={12}
                className={`mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 sm:text-sm font-mono text-sm ${
                  formErrors.eventData ? "border-red-300" : ""
                }`}
                placeholder="Enter valid JSON..."
              />
              {formErrors.eventData && (
                <p className="mt-1 text-sm text-red-600">
                  {formErrors.eventData}
                </p>
              )}
            </div>

            {/* Submit Button */}
            <div>
              <Button
                type="submit"
                disabled={isLoading || destinationsLoading}
                className="w-full"
                size="lg"
              >
                {isLoading ? (
                  <div className="flex items-center">
                    <div className="animate-spin -ml-1 mr-3 h-5 w-5 text-white">
                      <svg
                        className="h-5 w-5"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 24 24"
                      >
                        <circle
                          className="opacity-25"
                          cx="12"
                          cy="12"
                          r="10"
                          stroke="currentColor"
                          strokeWidth="4"
                        ></circle>
                        <path
                          className="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        ></path>
                      </svg>
                    </div>
                    Triggering Event...
                  </div>
                ) : (
                  "Trigger Event"
                )}
              </Button>
            </div>
            </form>
          </CardContent>
        </Card>

        {/* Response Display Area */}
        <Card>
          <CardHeader>
            <CardTitle>Response</CardTitle>
          </CardHeader>
          <CardContent>
            {response ? (
              <div className="space-y-4">
                {/* Status Badge */}
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
                  {response.timestamp && (
                    <span className="ml-2 text-xs text-gray-500">
                      {new Date(response.timestamp).toLocaleString()}
                    </span>
                  )}
                </div>

                {/* Message */}
                {(response.message || response.error) && (
                  <div
                    className={`p-4 rounded-md ${
                      response.success
                        ? "bg-green-50 text-green-700"
                        : "bg-red-50 text-red-700"
                    }`}
                  >
                    <p className="text-sm">
                      {response.message || response.error}
                    </p>
                  </div>
                )}

                {/* Success Details */}
                {response.success &&
                  (response.eventId ||
                    response.destinationId ||
                    response.topic) && (
                    <div className="bg-gray-50 border border-gray-200 rounded-md p-4">
                      <h3 className="text-sm font-medium text-gray-900 mb-2">
                        Event Details:
                      </h3>
                      <dl className="text-sm space-y-1">
                        {response.eventId && (
                          <div className="flex">
                            <dt className="font-medium text-gray-600 w-20">
                              Event ID:
                            </dt>
                            <dd className="text-gray-900 font-mono text-xs">
                              {response.eventId}
                            </dd>
                          </div>
                        )}
                        {response.topic && (
                          <div className="flex">
                            <dt className="font-medium text-gray-600 w-20">
                              Topic:
                            </dt>
                            <dd className="text-gray-900">{response.topic}</dd>
                          </div>
                        )}
                        {response.destinationId && (
                          <div className="flex">
                            <dt className="font-medium text-gray-600 w-20">
                              Destination:
                            </dt>
                            <dd className="text-gray-900 font-mono text-xs">
                              {response.destinationId}
                            </dd>
                          </div>
                        )}
                      </dl>
                    </div>
                  )}

                {/* Error Details */}
                {!response.success && response.details && (
                  <div>
                    <h3 className="text-sm font-medium text-gray-900 mb-2">
                      Error Details:
                    </h3>
                    <pre className="bg-gray-50 border rounded-md p-4 text-sm overflow-auto text-red-800">
                      {typeof response.details === "string"
                        ? response.details
                        : JSON.stringify(response.details, null, 2)}
                    </pre>
                  </div>
                )}
              </div>
            ) : (
              <div className="text-center py-12">
                <svg
                  className="mx-auto h-12 w-12 text-gray-400"
                  stroke="currentColor"
                  fill="none"
                  viewBox="0 0 48 48"
                >
                  <path
                    d="M8 14v20c0 4.418 7.163 8 16 8 1.381 0 2.721-.087 4-.252M8 14c0 4.418 7.163 8 16 8s16-3.582 16-8M8 14c0-4.418 7.163-8 16-8s16 3.582 16 8m0 0v14m0-4c0 4.418-7.163 8-16 8S8 28.418 8 24"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
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
