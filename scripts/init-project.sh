#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if module name is provided
if [ -z "$1" ]; then
    echo -e "${RED}Error: Module name is required${NC}"
    echo "Usage: ./init-project.sh github.com/username/project"
    exit 1
fi

NEW_MODULE=$1
OLD_MODULE="github.com/mr-kaynak/go-core"

echo -e "${YELLOW}Initializing project with module: ${NEW_MODULE}${NC}"

# Function to replace module name in files
replace_in_files() {
    local file_pattern=$1
    echo -e "${YELLOW}Updating ${file_pattern} files...${NC}"

    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        find . -type f -name "${file_pattern}" -not -path "./vendor/*" -not -path "./.git/*" -exec sed -i '' "s|${OLD_MODULE}|${NEW_MODULE}|g" {} +
    else
        # Linux
        find . -type f -name "${file_pattern}" -not -path "./vendor/*" -not -path "./.git/*" -exec sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" {} +
    fi
}

# Update go.mod
echo -e "${YELLOW}Updating go.mod...${NC}"
if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|${OLD_MODULE}|${NEW_MODULE}|g" go.mod
else
    sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" go.mod
fi

# Update all Go files
replace_in_files "*.go"

# Update Makefile if needed
echo -e "${YELLOW}Updating Makefile...${NC}"
if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|${OLD_MODULE}|${NEW_MODULE}|g" Makefile
else
    sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" Makefile
fi

# Update README.md
echo -e "${YELLOW}Updating README.md...${NC}"
if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|${OLD_MODULE}|${NEW_MODULE}|g" README.md
else
    sed -i "s|${OLD_MODULE}|${NEW_MODULE}|g" README.md
fi

# Update docker-compose.yml if needed
if [ -f "docker-compose.yml" ]; then
    echo -e "${YELLOW}Updating docker-compose.yml...${NC}"
    PROJECT_NAME=$(echo $NEW_MODULE | awk -F'/' '{print $NF}')
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|go-core|${PROJECT_NAME}|g" docker-compose.yml
    else
        sed -i "s|go-core|${PROJECT_NAME}|g" docker-compose.yml
    fi
fi

# Create .env file from .env.example if it doesn't exist
if [ ! -f ".env" ] && [ -f ".env.example" ]; then
    echo -e "${YELLOW}Creating .env file from .env.example...${NC}"
    cp .env.example .env
    PROJECT_NAME=$(echo $NEW_MODULE | awk -F'/' '{print $NF}')
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|go-core|${PROJECT_NAME}|g" .env
    else
        sed -i "s|go-core|${PROJECT_NAME}|g" .env
    fi
fi

# Initialize git if not already initialized
if [ ! -d ".git" ]; then
    echo -e "${YELLOW}Initializing git repository...${NC}"
    git init
    git add .
    git commit -m "Initial commit from go-core boilerplate"
fi

# Run go mod tidy to update dependencies
echo -e "${YELLOW}Tidying Go modules...${NC}"
go mod tidy

echo -e "${GREEN}✅ Project initialized successfully!${NC}"
echo -e "${GREEN}Module name changed from ${OLD_MODULE} to ${NEW_MODULE}${NC}"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo "1. Review and update .env file with your configuration"
echo "2. Run 'make docker-up' to start infrastructure services"
echo "3. Run 'make migrate' to create database tables"
echo "4. Run 'make run' to start the API server"
echo "5. Visit http://localhost:3000/health to verify the setup"
echo ""
echo -e "${GREEN}Happy coding! 🚀${NC}"