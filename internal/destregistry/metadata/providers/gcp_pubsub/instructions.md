# GCP Pub/Sub Destination

This guide provides comprehensive instructions for setting up a GCP Pub/Sub destination using the gcloud CLI.

## Prerequisites

- **gcloud CLI**: Install the [Google Cloud CLI](https://cloud.google.com/sdk/docs/install)
- **GCP Account**: A Google Cloud Platform account
- **Permissions**: You need sufficient permissions to create projects (if creating new), topics, service accounts, and assign IAM roles

## Authentication

Before proceeding with the setup, you must authenticate the gcloud CLI with your Google account.

### Authenticate with User Credentials

```bash
# Login to your Google account
gcloud auth login

# This will open a browser window for authentication
# Follow the prompts to complete the login process
```

### Set Application Default Credentials (Recommended)

For running applications that use GCP services, also set up Application Default Credentials:

```bash
# Set application default credentials
gcloud auth application-default login

# This ensures your local development environment can authenticate
# with GCP services using your user account
```

### Verify Authentication

```bash
# Check currently authenticated account
gcloud auth list

# Verify active configuration
gcloud config list
```

**Note**: The `gcloud auth login` command authenticates your user account for running gcloud CLI commands. The `gcloud auth application-default login` command sets up credentials that applications (including Outpost during local testing) can use to authenticate with GCP services.

## Setup Instructions

### 1. Create a GCP Project (Optional)

If you don't have an existing GCP project, you can create one using the CLI:

```bash
# Set your desired project ID (must be globally unique)
export PROJECT_ID="outpost-test-$(date +%s)"

# Create a new project
gcloud projects create $PROJECT_ID --name="Outpost Project"

# List your projects to verify
# This can take a few moments to propagate
gcloud projects list

# Link a billing account (required for Pub/Sub)
# First, list available billing accounts
gcloud billing accounts list

# Set your billing account ID from the previous command
export BILLING_ACCOUNT_ID="your-billing-account-id"

# Link billing account to project (replace BILLING_ACCOUNT_ID)
gcloud billing projects link $PROJECT_ID \
    --billing-account=$BILLING_ACCOUNT_ID
```

**Note**: If you already have a project, skip to step 2.

### 2. Set Your GCP Project

Set the project ID as a variable and configure gcloud to use it:

```bash
# Set your GCP project ID if you haven't already
export PROJECT_ID="your-project-id"

# Configure gcloud to use the project
gcloud config set project $PROJECT_ID

# Set the quota project for Application Default Credentials
# This prevents quota warnings when using the project
gcloud auth application-default set-quota-project $PROJECT_ID
```

### 3. Enable the Pub/Sub API

Enable the Pub/Sub API for your project:

```bash
gcloud services enable pubsub.googleapis.com
```

### 4. Create a Pub/Sub Topic

Create a new Pub/Sub topic where events will be published:

```bash
# Set your topic name
export TOPIC_NAME="outpost-events"

# Create the topic
gcloud pubsub topics create $TOPIC_NAME

# Verify the topic was created
gcloud pubsub topics list
```

### 5. Create a Service Account

Create a dedicated service account for Outpost to use when publishing to Pub/Sub:

```bash
# Set service account name
export SERVICE_ACCOUNT_NAME="outpost-pubsub-publisher"

# Create the service account
gcloud iam service-accounts create $SERVICE_ACCOUNT_NAME \
    --display-name="Outpost Pub/Sub Publisher" \
    --description="Service account for Outpost to publish messages to Pub/Sub"

# Verify the service account was created
gcloud iam service-accounts list
```

### 6. Grant Pub/Sub Publisher Permissions

Assign the Pub/Sub Publisher role to the service account:

```bash
# Grant Pub/Sub Publisher role at the project level
gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/pubsub.publisher"

# Alternatively, grant permissions only for the specific topic (more restrictive)
gcloud pubsub topics add-iam-policy-binding $TOPIC_NAME \
    --member="serviceAccount:${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/pubsub.publisher"
```

### 7. Create and Download Service Account Keys

Generate a JSON key file for the service account:

```bash
# Create and download the service account key
gcloud iam service-accounts keys create ~/outpost-pubsub-key.json \
    --iam-account="${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# Verify the key was created
ls -la ~/outpost-pubsub-key.json
```

**Important**: Store this key file securely. It provides authentication credentials for your service account.

## Configuration

When configuring your Outpost destination, you'll need:

1. **Project ID**: Your GCP project ID (e.g., `my-gcp-project`)
2. **Topic Name**: The name of your Pub/Sub topic (e.g., `outpost-events`)
3. **Service Account Credentials**: The contents of the JSON key file or the path to it

### Example Configuration

```json
{
  "type": "gcp_pubsub",
  "config": {
    "project_id": "your-project-id",
    "topic_name": "outpost-events",
    "credentials_json": "contents-of-key-file"
  }
}
```

## Testing the Integration

### Create a Test Subscription

Create a subscription to verify messages are being published:

```bash
# Create a test subscription
export SUBSCRIPTION_NAME="outpost-events-test"

gcloud pubsub subscriptions create $SUBSCRIPTION_NAME \
    --topic=$TOPIC_NAME \
    --ack-deadline=60

# Pull messages from the subscription to test
gcloud pubsub subscriptions pull $SUBSCRIPTION_NAME \
    --auto-ack \
    --limit=10
```

### Publish a Test Message

Test publishing directly to verify permissions:

```bash
# Publish a test message
gcloud pubsub topics publish $TOPIC_NAME \
    --message="Test message from Outpost setup"

# Pull the message to verify
gcloud pubsub subscriptions pull $SUBSCRIPTION_NAME \
    --auto-ack \
    --limit=1
```

### Verify Service Account Permissions

Check the IAM policy bindings:

```bash
# View project-level IAM policy for the service account
gcloud projects get-iam-policy $PROJECT_ID \
    --flatten="bindings[].members" \
    --filter="bindings.members:serviceAccount:${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# View topic-level IAM policy
gcloud pubsub topics get-iam-policy $TOPIC_NAME
```

## Troubleshooting

### Permission Denied Errors

If you encounter permission errors:

1. Verify the service account has the correct role:
   ```bash
   gcloud projects get-iam-policy $PROJECT_ID \
       --flatten="bindings[].members" \
       --filter="bindings.members:serviceAccount:${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
   ```

2. Ensure the Pub/Sub API is enabled:
   ```bash
   gcloud services list --enabled | grep pubsub
   ```

3. Check that the credentials file path is correct:
   ```bash
   cat $GOOGLE_APPLICATION_CREDENTIALS
   ```

### Topic Not Found Errors

Verify the topic exists and is in the correct project:

```bash
gcloud pubsub topics describe $TOPIC_NAME
```

### Authentication Issues

Validate your service account key:

```bash
# Test authentication with the service account
gcloud auth activate-service-account \
    --key-file=$GOOGLE_APPLICATION_CREDENTIALS

# List topics to verify access
gcloud pubsub topics list
```

### Message Delivery Issues

1. Check topic configuration:
   ```bash
   gcloud pubsub topics describe $TOPIC_NAME
   ```

2. Monitor message metrics:
   ```bash
   gcloud pubsub topics list-subscriptions $TOPIC_NAME
   ```

3. Review Cloud Logging for errors:
   ```bash
   gcloud logging read "resource.type=pubsub_topic AND resource.labels.topic_id=$TOPIC_NAME" \
       --limit=50 \
       --format=json
   ```

## Cleanup (Optional)

To remove the resources created during setup:

```bash
# Delete the subscription (if created for testing)
gcloud pubsub subscriptions delete $SUBSCRIPTION_NAME

# Delete the service account key
gcloud iam service-accounts keys list \
    --iam-account="${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
# Note the KEY_ID and delete it
gcloud iam service-accounts keys delete KEY_ID \
    --iam-account="${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# Remove IAM policy binding
gcloud projects remove-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/pubsub.publisher"

# Delete the service account
gcloud iam service-accounts delete \
    "${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# Delete the topic
gcloud pubsub topics delete $TOPIC_NAME
```

## Additional Resources

- [GCP Pub/Sub Documentation](https://cloud.google.com/pubsub/docs)
- [gcloud pubsub Command Reference](https://cloud.google.com/sdk/gcloud/reference/pubsub)
- [Service Account Best Practices](https://cloud.google.com/iam/docs/best-practices-service-accounts)
- [Pub/Sub Authentication Guide](https://cloud.google.com/pubsub/docs/authentication)
- [IAM Roles for Pub/Sub](https://cloud.google.com/pubsub/docs/access-control)
- [Pub/Sub Monitoring](https://cloud.google.com/pubsub/docs/monitoring)