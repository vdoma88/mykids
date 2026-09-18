-- CreateTable
CREATE TABLE "TamperEvent" (
    "id" TEXT NOT NULL,
    "childId" TEXT NOT NULL,
    "deviceId" TEXT,
    "kind" TEXT NOT NULL,
    "detail" TEXT,
    "recordedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "reviewedAt" TIMESTAMP(3),

    CONSTRAINT "TamperEvent_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE INDEX "TamperEvent_childId_recordedAt_idx" ON "TamperEvent"("childId", "recordedAt");

-- CreateIndex
CREATE INDEX "TamperEvent_childId_reviewedAt_idx" ON "TamperEvent"("childId", "reviewedAt");

-- AddForeignKey
ALTER TABLE "TamperEvent" ADD CONSTRAINT "TamperEvent_childId_fkey" FOREIGN KEY ("childId") REFERENCES "Child"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "TamperEvent" ADD CONSTRAINT "TamperEvent_deviceId_fkey" FOREIGN KEY ("deviceId") REFERENCES "Device"("id") ON DELETE SET NULL ON UPDATE CASCADE;
