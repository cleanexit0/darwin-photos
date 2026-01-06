#import <Foundation/Foundation.h>
#import <Photos/Photos.h>
#import "bridge.h"

PKAuthStatus PKCheckAuthorization(void) {
    PHAuthorizationStatus status = [PHPhotoLibrary authorizationStatusForAccessLevel:PHAccessLevelReadWrite];
    return (PKAuthStatus)status;
}

PKAuthStatus PKRequestAuthorization(void) {
    __block PHAuthorizationStatus resultStatus = PHAuthorizationStatusNotDetermined;
    dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

    [PHPhotoLibrary requestAuthorizationForAccessLevel:PHAccessLevelReadWrite handler:^(PHAuthorizationStatus status) {
        resultStatus = status;
        dispatch_semaphore_signal(semaphore);
    }];

    dispatch_semaphore_wait(semaphore, DISPATCH_TIME_FOREVER);
    return (PKAuthStatus)resultStatus;
}

PKResult PKDownloadAsset(const char *localIdentifier, const char *outputPath, char **errorOut) {
    @autoreleasepool {
        NSString *identifier = [NSString stringWithUTF8String:localIdentifier];
        NSString *path = [NSString stringWithUTF8String:outputPath];

        // Fetch the asset
        PHFetchResult<PHAsset *> *result = [PHAsset fetchAssetsWithLocalIdentifiers:@[identifier] options:nil];

        if (result.count == 0) {
            if (errorOut) {
                *errorOut = strdup("Asset not found");
            }
            return PKResultErrorNotFound;
        }

        PHAsset *asset = result.firstObject;

        // Configure request options
        PHImageRequestOptions *options = [[PHImageRequestOptions alloc] init];
        options.networkAccessAllowed = YES;  // Allow iCloud download
        options.deliveryMode = PHImageRequestOptionsDeliveryModeHighQualityFormat;
        options.synchronous = YES;
        options.version = PHImageRequestOptionsVersionOriginal;

        __block PKResult resultCode = PKResultSuccess;
        __block NSString *errorMessage = nil;

        // Request the image data
        [[PHImageManager defaultManager] requestImageDataAndOrientationForAsset:asset
                                                                        options:options
                                                                  resultHandler:^(NSData *imageData,
                                                                                  NSString *dataUTI,
                                                                                  CGImagePropertyOrientation orientation,
                                                                                  NSDictionary *info) {
            if (imageData) {
                NSError *writeError = nil;
                BOOL success = [imageData writeToFile:path options:NSDataWritingAtomic error:&writeError];
                if (!success) {
                    resultCode = PKResultErrorWriteFailed;
                    errorMessage = [NSString stringWithFormat:@"Failed to write file: %@", writeError.localizedDescription];
                }
            } else {
                NSError *error = info[PHImageErrorKey];
                if (error) {
                    errorMessage = [NSString stringWithFormat:@"Download failed: %@", error.localizedDescription];
                } else {
                    errorMessage = @"Download failed: unknown error";
                }
                resultCode = PKResultErrorDownloadFailed;
            }
        }];

        if (errorOut && errorMessage) {
            *errorOut = strdup([errorMessage UTF8String]);
        }

        return resultCode;
    }
}

PKResult PKDownloadVideoAsset(const char *localIdentifier, const char *outputPath, char **errorOut) {
    @autoreleasepool {
        NSString *identifier = [NSString stringWithUTF8String:localIdentifier];
        NSString *path = [NSString stringWithUTF8String:outputPath];

        // Fetch the asset
        PHFetchResult<PHAsset *> *result = [PHAsset fetchAssetsWithLocalIdentifiers:@[identifier] options:nil];

        if (result.count == 0) {
            if (errorOut) {
                *errorOut = strdup("Asset not found");
            }
            return PKResultErrorNotFound;
        }

        PHAsset *asset = result.firstObject;

        if (asset.mediaType != PHAssetMediaTypeVideo) {
            if (errorOut) {
                *errorOut = strdup("Asset is not a video");
            }
            return PKResultErrorUnknown;
        }

        // Configure video request options
        PHVideoRequestOptions *options = [[PHVideoRequestOptions alloc] init];
        options.networkAccessAllowed = YES;  // Allow iCloud download
        options.deliveryMode = PHVideoRequestOptionsDeliveryModeHighQualityFormat;
        options.version = PHVideoRequestOptionsVersionOriginal;

        __block PKResult resultCode = PKResultSuccess;
        __block NSString *errorMessage = nil;
        dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

        // Request the video asset
        [[PHImageManager defaultManager] requestAVAssetForVideo:asset
                                                        options:options
                                                  resultHandler:^(AVAsset *avAsset,
                                                                  AVAudioMix *audioMix,
                                                                  NSDictionary *info) {
            if (avAsset) {
                // For URL-based assets, copy the file
                if ([avAsset isKindOfClass:[AVURLAsset class]]) {
                    AVURLAsset *urlAsset = (AVURLAsset *)avAsset;
                    NSError *copyError = nil;

                    // Remove existing file if present
                    [[NSFileManager defaultManager] removeItemAtPath:path error:nil];

                    BOOL success = [[NSFileManager defaultManager] copyItemAtURL:urlAsset.URL
                                                                           toURL:[NSURL fileURLWithPath:path]
                                                                           error:&copyError];
                    if (!success) {
                        resultCode = PKResultErrorWriteFailed;
                        errorMessage = [NSString stringWithFormat:@"Failed to copy video: %@", copyError.localizedDescription];
                    }
                } else {
                    resultCode = PKResultErrorUnknown;
                    errorMessage = @"Unsupported video asset type";
                }
            } else {
                NSError *error = info[PHImageErrorKey];
                if (error) {
                    errorMessage = [NSString stringWithFormat:@"Video download failed: %@", error.localizedDescription];
                } else {
                    errorMessage = @"Video download failed: unknown error";
                }
                resultCode = PKResultErrorDownloadFailed;
            }

            dispatch_semaphore_signal(semaphore);
        }];

        dispatch_semaphore_wait(semaphore, DISPATCH_TIME_FOREVER);

        if (errorOut && errorMessage) {
            *errorOut = strdup([errorMessage UTF8String]);
        }

        return resultCode;
    }
}
