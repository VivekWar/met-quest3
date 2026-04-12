# MET-QUEST '26: Smart Alloy Selector
## Project Charter & Capabilities Mapping

This document demonstrates how the architectural design and code implementation of the Smart Alloy Selector directly fulfills and exceeds the competition objectives.

---

### 1. Core Objective: The Virtual Materials Scientist
**Goal:** Develop an AI-based tool to act as a virtual materials scientist, parsing constraints and recommending optimal materials from a database.
**Implementation:** 
* We leveraged a Retrieval-Augmented Generation (RAG) architecture. 
* A natural language query is translated by the LLM into a rigorous SQL structural query against a local PostgreSQL database (seeded with 455 aerospace and industrial materials). 
* The constraints are extracted dynamically, enabling the system to act as a true "scientist" weighing thermal, mechanical, and electrical filters simultaneously.

### 2. Solving the "Data-Stack" Bottleneck
**Goal:** Replace static databases (like MatWeb) that require manual scraping.
**Implementation:** 
* Instead of manual tables, the database features rapid SQL indexing over 13 physical properties.
* The frontend (`PredictorPanel` and `QueryInput`) automatically interpolates user needs. What previously took hours of disjointed table-scraping now takes milliseconds using the `POST /api/v1/recommend` endpoint, returning the top 3 statistically valid candidates.

### 3. Functional Requirements: Holistic Evaluation
**Goal:** Evaluate multiple properties against harsh realities (Thermal, Electrical, Fatigue, Mechanical).
**Implementation:**
* The database natively stores *thermal expansion*, *melting point*, *electrical resistivity*, and *yield/tensile strength*.
* Additionally, the **AI Reframer** step evaluates implicit criteria: the `Reframe` function explicitly instructs the LLM to analyze and formulate explanations regarding the chosen material's **fatigue limits** and **corrosion resistance** based on its microstructural profile, perfectly mapping to the evaluation goals.

### 4. Advanced Features ("Brownie Points")
We captured every advanced criteria specified in the competition rubric:
1.  **Natural Language Interface:** The UI allows conversational inputs (e.g., *"Our robotics team is designing a custom mounting bracket that gets hot"*). The backend intelligently maps "gets hot" to `melting_point > 400K` without the user writing code.
2.  **Expert Explanations:** The Virtual Scientist LLM generates cleanly formatted Markdown reports outlining **why** a material won, comparing it to runners-up in a table, and noting real-world **Engineering Trade-offs**.
3.  **Predictive Modeling:** The `POST /api/v1/predict` endpoint takes a bespoke alloy composition from the UI sliders, calculates a physics-based Rule-of-Mixtures, and utilizes LLM reasoning to adjust for phase diagrams and intermetallic variations for unprecedented alloys.

### 5. Deployment & Scalability 
The system is built on scalable, modern infrastructure—perfect for competition scaling:
*   **Database:** Serverless Neon PostgreSQL (Zero hardware management).
*   **Backend:** High-concurrency Go / Gin API running on Google Cloud Run.
*   **Frontend:** React (Vite) hosted globally at the edge via Firebase Hosting. 

Everything you outlined in the goals has been architected, programmed, and documented!
