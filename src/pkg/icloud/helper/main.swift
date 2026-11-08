import Foundation

// MARK: - iCloud Status Types

enum ICloudStatus: String {
    case unknown = "unknown"
    case notICloud = "not_icloud"
    case synced = "synced"
    case pendingUpload = "pending_upload"
    case cloudOnly = "cloud_only"
    case downloading = "downloading"
    case uploading = "uploading"
}

// MARK: - Status Check

func getICloudStatus(path: String) -> ICloudStatus {
    let url = URL(fileURLWithPath: path)

    // Check if this is in an iCloud folder
    if !isICloudPath(path) {
        return .notICloud
    }

    // Handle .icloud placeholder files (cloud-only files)
    if path.contains(".icloud") {
        return .cloudOnly
    }

    do {
        let keys: Set<URLResourceKey> = [
            .ubiquitousItemDownloadingStatusKey,
            .ubiquitousItemIsUploadedKey,
            .ubiquitousItemIsUploadingKey,
            .ubiquitousItemIsDownloadingKey
        ]
        let values = try url.resourceValues(forKeys: keys)

        // Check if currently uploading
        if values.ubiquitousItemIsUploading == true {
            return .uploading
        }

        // Check if currently downloading
        if values.ubiquitousItemIsDownloading == true {
            return .downloading
        }

        // Check download status
        if let downloadStatus = values.ubiquitousItemDownloadingStatus {
            switch downloadStatus {
            case .notDownloaded:
                return .cloudOnly
            case .downloaded, .current:
                // File is local, check upload status
                if values.ubiquitousItemIsUploaded == true {
                    return .synced
                } else {
                    return .pendingUpload
                }
            default:
                break
            }
        }

        // If we can't determine, assume synced if file exists locally
        if FileManager.default.fileExists(atPath: path) {
            if values.ubiquitousItemIsUploaded == true {
                return .synced
            }
            return .pendingUpload
        }

        return .unknown
    } catch {
        // If we can't get resource values, check if file exists
        if FileManager.default.fileExists(atPath: path) {
            return .pendingUpload
        }
        return .unknown
    }
}

func isICloudPath(_ path: String) -> Bool {
    // Check if path is in iCloud Mobile Documents folder
    return path.contains("/Library/Mobile Documents/")
}

func isICloudFolder(_ path: String) -> Bool {
    // Check if the folder is within iCloud
    if !isICloudPath(path) {
        return false
    }

    // Verify it exists
    var isDir: ObjCBool = false
    return FileManager.default.fileExists(atPath: path, isDirectory: &isDir) && isDir.boolValue
}

// MARK: - Operations

func downloadFile(path: String) -> Bool {
    let url = URL(fileURLWithPath: path)

    // Handle .icloud placeholder files
    var actualPath = path
    if !path.contains(".icloud") {
        // Check if there's a .icloud version
        let fileName = url.lastPathComponent
        let parentPath = url.deletingLastPathComponent().path
        let icloudName = ".\(fileName).icloud"
        let icloudPath = (parentPath as NSString).appendingPathComponent(icloudName)
        if FileManager.default.fileExists(atPath: icloudPath) {
            actualPath = icloudPath
        }
    }

    let actualURL = URL(fileURLWithPath: actualPath)

    do {
        try FileManager.default.startDownloadingUbiquitousItem(at: actualURL)
        return true
    } catch {
        fputs("Error downloading: \(error.localizedDescription)\n", stderr)
        return false
    }
}

func evictFile(path: String) -> Bool {
    let url = URL(fileURLWithPath: path)

    do {
        try FileManager.default.evictUbiquitousItem(at: url)
        return true
    } catch {
        fputs("Error evicting: \(error.localizedDescription)\n", stderr)
        return false
    }
}

func triggerUpload(path: String) -> Bool {
    // Trigger upload by having Finder select the file
    // This triggers iCloud's file coordination and starts the upload
    // Note: This briefly selects the file in Finder but doesn't open any windows

    // Check if file exists
    guard FileManager.default.fileExists(atPath: path) else {
        return false
    }

    // Use AppleScript to have Finder select the file
    // This triggers iCloud upload without opening any new windows
    let script = "tell application \"Finder\" to select POSIX file \"\(path)\""

    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
    process.arguments = ["-e", script]
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice

    do {
        try process.run()
        process.waitUntilExit()
        return process.terminationStatus == 0
    } catch {
        return false
    }
}

// Restart iCloud sync daemon to force pending uploads
func restartICloudDaemon() {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/killall")
    process.arguments = ["bird"]

    // Suppress output - bird may not be running
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice

    try? process.run()
    process.waitUntilExit()
}

// MARK: - Batch Operations

func batchStatus(paths: [String]) -> [String: String] {
    var results: [String: String] = [:]
    for path in paths {
        results[path] = getICloudStatus(path: path).rawValue
    }
    return results
}

func batchDownload(paths: [String]) -> (success: Int, failed: Int) {
    var success = 0
    var failed = 0

    let group = DispatchGroup()
    let queue = DispatchQueue(label: "icloud.download", attributes: .concurrent)
    let lock = NSLock()

    for path in paths {
        group.enter()
        queue.async {
            if downloadFile(path: path) {
                lock.lock()
                success += 1
                lock.unlock()
            } else {
                lock.lock()
                failed += 1
                lock.unlock()
            }
            group.leave()
        }
    }

    group.wait()
    return (success, failed)
}

func batchEvict(paths: [String]) -> (success: Int, failed: Int) {
    var success = 0
    var failed = 0

    let group = DispatchGroup()
    let queue = DispatchQueue(label: "icloud.evict", attributes: .concurrent)
    let lock = NSLock()

    for path in paths {
        group.enter()
        queue.async {
            if evictFile(path: path) {
                lock.lock()
                success += 1
                lock.unlock()
            } else {
                lock.lock()
                failed += 1
                lock.unlock()
            }
            group.leave()
        }
    }

    group.wait()
    return (success, failed)
}

func batchUpload(paths: [String]) -> (success: Int, failed: Int) {
    var success = 0
    var failed = 0

    // Process each file individually with Finder select
    // Note: Selecting a folder only triggers upload for files that existed when
    // the folder was first synced, not for newly added files. So we must select
    // each file directly to ensure reliable upload triggering.
    for path in paths {
        if triggerUpload(path: path) {
            success += 1
            // Brief delay between files to let iCloud register each one
            Thread.sleep(forTimeInterval: 0.3)
        } else {
            failed += 1
        }
    }

    return (success, failed)
}

// MARK: - JSON Output

func outputJSON(_ value: Any) {
    if let data = try? JSONSerialization.data(withJSONObject: value, options: []),
       let str = String(data: data, encoding: .utf8) {
        print(str)
    }
}

// MARK: - Main

func printUsage() {
    let usage = """
    icloud-helper - iCloud status and operations helper for superfile

    Usage:
      icloud-helper status <path>              Get iCloud status for a file
      icloud-helper batch-status <path>...     Get status for multiple files (JSON output)
      icloud-helper is-icloud-folder <path>    Check if path is in iCloud
      icloud-helper download <path>            Download file from iCloud
      icloud-helper batch-download <path>...   Download multiple files
      icloud-helper evict <path>               Evict local copy
      icloud-helper batch-evict <path>...      Evict multiple files
      icloud-helper upload <path>              Trigger upload for a file
      icloud-helper batch-upload <path>...     Trigger upload for multiple files
      icloud-helper sync                       Restart iCloud daemon to force sync

    Status values:
      unknown        - Could not determine status
      not_icloud     - File is not in an iCloud folder
      synced         - File is uploaded and current
      pending_upload - Local changes not yet uploaded
      cloud_only     - File exists only in cloud
      downloading    - Currently downloading
      uploading      - Currently uploading
    """
    print(usage)
}

let args = Array(CommandLine.arguments.dropFirst())

guard !args.isEmpty else {
    printUsage()
    exit(1)
}

let command = args[0]
let paths = Array(args.dropFirst())

switch command {
case "status":
    guard let path = paths.first else {
        fputs("Error: path required\n", stderr)
        exit(1)
    }
    print(getICloudStatus(path: path).rawValue)

case "batch-status":
    guard !paths.isEmpty else {
        fputs("Error: at least one path required\n", stderr)
        exit(1)
    }
    outputJSON(batchStatus(paths: paths))

case "is-icloud-folder":
    guard let path = paths.first else {
        fputs("Error: path required\n", stderr)
        exit(1)
    }
    print(isICloudFolder(path) ? "true" : "false")

case "download":
    guard let path = paths.first else {
        fputs("Error: path required\n", stderr)
        exit(1)
    }
    exit(downloadFile(path: path) ? 0 : 1)

case "batch-download":
    guard !paths.isEmpty else {
        fputs("Error: at least one path required\n", stderr)
        exit(1)
    }
    let (success, failed) = batchDownload(paths: paths)
    outputJSON(["success": success, "failed": failed])

case "evict":
    guard let path = paths.first else {
        fputs("Error: path required\n", stderr)
        exit(1)
    }
    exit(evictFile(path: path) ? 0 : 1)

case "batch-evict":
    guard !paths.isEmpty else {
        fputs("Error: at least one path required\n", stderr)
        exit(1)
    }
    let (success, failed) = batchEvict(paths: paths)
    outputJSON(["success": success, "failed": failed])

case "upload":
    guard let path = paths.first else {
        fputs("Error: path required\n", stderr)
        exit(1)
    }
    exit(triggerUpload(path: path) ? 0 : 1)

case "batch-upload":
    guard !paths.isEmpty else {
        fputs("Error: at least one path required\n", stderr)
        exit(1)
    }
    let (success, failed) = batchUpload(paths: paths)
    outputJSON(["success": success, "failed": failed])

case "sync":
    restartICloudDaemon()
    print("iCloud daemon restarted")

case "help", "-h", "--help":
    printUsage()

default:
    fputs("Unknown command: \(command)\n", stderr)
    printUsage()
    exit(1)
}
